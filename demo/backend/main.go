//go:build js && wasm

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"syscall/js"
	"time"

	"github.com/asciimoth/configer/hujson"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

type backend struct {
	mu             sync.Mutex
	w              *pasta.Workspace
	subscriptionID uint64
	configText     string
	logs           []logEntry
}

type logEntry struct {
	At     string `json:"at"`
	Source string `json:"source"`
	Level  string `json:"level"`
	Text   string `json:"text"`
}

type callRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type callResponse struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Result any    `json:"result,omitempty"`
}

var app = &backend{configText: initialConfig}

func main() {
	done := make(chan struct{})
	app.restart(initialConfig)
	js.Global().Set("pastaBackendCall", js.FuncOf(app.call))
	app.emitLog("frontend", "info", "Go WASM backend exported pastaBackendCall")
	<-done
}

func (b *backend) restart(configText string) {
	cfg, err := hujson.Parse([]byte(configText))
	if err != nil {
		b.emitLog("backend", "error", "config parse failed: "+err.Error())
		return
	}
	w, err := pasta.WorkspaceFromConfig(Classes(), cfg, demoLogFactory{b: b})
	if err != nil {
		b.emitLog("backend", "error", "workspace restore failed: "+err.Error())
		return
	}
	b.mu.Lock()
	old := b.w
	b.w = w
	b.configText = configText
	b.mu.Unlock()
	if old != nil {
		old.Close()
	}
	b.subscriptionID = w.SubscribeNotifications(func(n pasta.WorkspaceNotification) {
		b.dispatch("notification", n)
		b.emitLog("workspace", "debug", fmt.Sprintf("%s %s", n.Kind, notificationTarget(n)))
	})
	b.emitLog("backend", "info", "workspace restored from config")
}

func (b *backend) call(_ js.Value, args []js.Value) any {
	if len(args) == 0 {
		return encodeResponse(callResponse{OK: false, Error: "missing request"})
	}
	var req callRequest
	if err := json.Unmarshal([]byte(args[0].String()), &req); err != nil {
		return encodeResponse(callResponse{OK: false, Error: err.Error()})
	}
	result, err := b.handle(req)
	if err != nil {
		b.emitLog("api", "warn", req.Method+": "+err.Error())
		return encodeResponse(callResponse{OK: false, Error: err.Error()})
	}
	b.emitLog("api", "debug", req.Method)
	return encodeResponse(callResponse{OK: true, Result: result})
}

func (b *backend) handle(req callRequest) (any, error) {
	switch req.Method {
	case "logs":
		b.mu.Lock()
		defer b.mu.Unlock()
		out := append([]logEntry(nil), b.logs...)
		return out, nil
	case "initialConfig":
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.configText, nil
	}

	w := b.workspace()
	if w == nil {
		return nil, errors.New("workspace is not ready")
	}
	switch req.Method {
	case "snapshot":
		return w.Snapshot(), nil
	case "saveConfig":
		cfg, err := hujson.Parse([]byte(b.configText))
		if err != nil {
			cfg, err = hujson.Parse([]byte(`{}`))
			if err != nil {
				return nil, err
			}
		}
		if err := w.SaveConfig(cfg); err != nil {
			return nil, err
		}
		text, err := formatHuJSONText(cfg.Pack())
		if err != nil {
			return nil, err
		}
		b.mu.Lock()
		b.configText = text
		b.mu.Unlock()
		return text, nil
	case "reloadConfig":
		var p struct {
			Config string `json:"config"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		b.restart(p.Config)
		return true, nil
	case "addNode":
		var p struct {
			Class    string `json:"class"`
			Position string `json:"position"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := w.AddNodeByClass(p.Class)
		if err != nil {
			return nil, err
		}
		if p.Position != "" {
			_ = w.SetNodePosition(id, p.Position)
		}
		return id, nil
	case "removeNodes":
		var p struct {
			IDs []uint64 `json:"ids"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		for _, id := range p.IDs {
			w.RemoveNode(id)
		}
		return true, nil
	case "setNodePosition":
		var p struct {
			ID       uint64 `json:"id"`
			Position string `json:"position"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return true, w.SetNodePosition(p.ID, p.Position)
	case "setNodeName":
		var p struct {
			ID   uint64 `json:"id"`
			Name string `json:"name"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return true, w.SetNodeName(p.ID, p.Name)
	case "addLink":
		var p struct {
			From uint64 `json:"from"`
			To   uint64 `json:"to"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		if port, ok := w.PortSnapshot(p.To); ok && !isMultiLinkTarget(port) {
			for _, link := range port.Links {
				w.RemoveLink(link)
			}
		}
		id, typ, err := w.AddLink(p.From, p.To)
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": id, "type": typ}, nil
	case "removeLink":
		var p struct {
			ID uint64 `json:"id"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		w.RemoveLink(p.ID)
		return true, nil
	case "removeNodePopup":
		var p struct {
			ID      uint64 `json:"id"`
			PopupID uint64 `json:"popupId"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return true, w.RemoveNodePopup(p.ID, p.PopupID)
	case "removeNodePopups":
		var p struct {
			ID uint64 `json:"id"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return true, w.RemoveNodePopups(p.ID)
	case "copy":
		var p struct {
			IDs []uint64 `json:"ids"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return w.Copy(p.IDs), nil
	case "paste":
		var p struct {
			Data string `json:"data"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return w.Paste(p.Data), nil
	case "undo":
		w.Undo()
		return true, nil
	case "redo":
		w.Redo()
		return true, nil
	case "classDescription":
		var p struct {
			Class string `json:"class"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		text, ok := w.NodeClassLongDescription(p.Class)
		if !ok {
			return "", errors.New("class description not found")
		}
		return text, nil
	case "subscribeNodeMenu":
		var p struct {
			ID uint64 `json:"id"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return w.SubscribeNodeMenu(p.ID, b.subscriptionID), nil
	case "unsubscribeNodeMenu":
		var p struct {
			ID uint64 `json:"id"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return w.UnsubscribeNodeMenu(p.ID, b.subscriptionID), nil
	case "trigger":
		var p struct {
			ID uint64 `json:"id"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return true, w.Trigger(p.ID)
	case "formular":
		var p struct {
			ID      uint64          `json:"id"`
			Message json.RawMessage `json:"message"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		msg, err := decodeFormular(p.Message)
		if err != nil {
			return nil, err
		}
		w.SendNodeFormularMsg(p.ID, msg)
		return true, nil
	default:
		return nil, errors.New("unknown method " + req.Method)
	}
}

func isMultiLinkTarget(port pasta.PortSnapshot) bool {
	return port.Direction == "left" && len(port.Types) == 1 && port.Types[0] == typeNetwork
}

func (b *backend) workspace() *pasta.Workspace {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.w
}

func decodeParams(data json.RawMessage, dst any) error {
	if len(data) == 0 {
		data = []byte(`{}`)
	}
	return json.Unmarshal(data, dst)
}

func decodeFormular(data json.RawMessage) (any, error) {
	var base formular.MessageBase
	if err := json.Unmarshal(data, &base); err != nil {
		return nil, err
	}
	switch base.Type {
	case formular.MessageFieldUpdate:
		var msg formular.FieldUpdateMessage
		return msg, json.Unmarshal(data, &msg)
	case formular.MessageFieldValidate:
		var msg formular.FieldValidateMessage
		return msg, json.Unmarshal(data, &msg)
	case formular.MessageFormApply:
		var msg formular.FormApplyMessage
		return msg, json.Unmarshal(data, &msg)
	case formular.MessageButtonPress:
		var msg formular.ButtonPressMessage
		return msg, json.Unmarshal(data, &msg)
	case formular.MessageAutocompleteRequest:
		var msg formular.AutocompleteRequestMessage
		return msg, json.Unmarshal(data, &msg)
	default:
		var msg map[string]any
		return msg, json.Unmarshal(data, &msg)
	}
}

func encodeResponse(resp callResponse) string {
	data, err := json.Marshal(resp)
	if err != nil {
		return `{"ok":false,"error":"response marshal failed"}`
	}
	return string(data)
}

func (b *backend) dispatch(kind string, payload any) {
	data, err := json.Marshal(map[string]any{"type": kind, "payload": payload})
	if err != nil {
		return
	}
	fn := js.Global().Get("pastaFrontendDispatch")
	if fn.Type() == js.TypeFunction {
		fn.Invoke(string(data))
	}
}

func (b *backend) emitLog(source, level, text string) {
	entry := logEntry{At: time.Now().Format("15:04:05.000"), Source: source, Level: level, Text: text}
	b.mu.Lock()
	b.logs = append(b.logs, entry)
	if len(b.logs) > 400 {
		b.logs = append([]logEntry(nil), b.logs[len(b.logs)-400:]...)
	}
	b.mu.Unlock()
	b.dispatch("log", entry)
}

func notificationTarget(n pasta.WorkspaceNotification) string {
	if n.ID != 0 {
		return fmt.Sprintf("#%d", n.ID)
	}
	if n.ClassName != "" {
		return n.ClassName
	}
	return ""
}

type demoLogFactory struct{ b *backend }
type demoLogger struct {
	b      *backend
	source string
}

func (f demoLogFactory) WorkspaceLogger() pasta.Logger {
	return demoLogger{b: f.b, source: "workspace"}
}
func (f demoLogFactory) NodeLogger(id uint64, class string) pasta.Logger {
	return demoLogger{b: f.b, source: fmt.Sprintf("node %d %s", id, class)}
}
func (l demoLogger) Debug(args ...any)                 { l.log("debug", fmt.Sprint(args...)) }
func (l demoLogger) Debugf(format string, args ...any) { l.log("debug", fmt.Sprintf(format, args...)) }
func (l demoLogger) Info(args ...any)                  { l.log("info", fmt.Sprint(args...)) }
func (l demoLogger) Infof(format string, args ...any)  { l.log("info", fmt.Sprintf(format, args...)) }
func (l demoLogger) Warn(args ...any)                  { l.log("warn", fmt.Sprint(args...)) }
func (l demoLogger) Warnf(format string, args ...any)  { l.log("warn", fmt.Sprintf(format, args...)) }
func (l demoLogger) Err(args ...any)                   { l.log("error", fmt.Sprint(args...)) }
func (l demoLogger) Errf(format string, args ...any)   { l.log("error", fmt.Sprintf(format, args...)) }
func (l demoLogger) Fatal(args ...any)                 { l.log("fatal", fmt.Sprint(args...)) }
func (l demoLogger) Fatalf(format string, args ...any) { l.log("fatal", fmt.Sprintf(format, args...)) }
func (l demoLogger) log(level, text string) {
	if l.b != nil {
		l.b.emitLog(l.source, level, text)
	}
}
