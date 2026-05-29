package main

import (
	"bytes"

	"github.com/asciimoth/pasta/pasta"
	"github.com/asciimoth/pasta/pasta/std"
	tailscalehujson "github.com/tailscale/hujson"
)

const initialConfig = `{
  // Positions are frontend-owned JSON strings. Pasta preserves them.
  "A": {
    "Class": "pasta/IntConstant",
    "Pos": "{\"x\":60,\"y\":80}",
    "value": 7,
    "Links": [
      "output -> [Sum] input 1",
      "output -> [Product] input 1",
      "output -> [Ratio] input 2",
      "output -> [IsAEqualB] input 1",
    ],
  },
  "B": {
    "Class": "pasta/IntConstant",
    "Pos": "{\"x\":60,\"y\":220}",
    "value": 5,
    "Links": [
      "output -> [Sum] input 2",
      "output -> [Product] input 2",
      "output -> [Difference] input 2",
      "output -> [IsAEqualB] input 2",
      "output -> [IsDifferenceNotB] input 2",
    ],
  },
  "FloatValue": {
    "Class": "pasta/FloatConstant",
    "Pos": "{\"x\":60,\"y\":360}",
    "value": 2.5,
    "Links": ["output -> [Summary] Float"],
  },
  "TrueGate": {
    "Class": "pasta/TrueConstant",
    "Pos": "{\"x\":60,\"y\":520}",
    "Links": ["output -> [AllChecks] input 1"],
  },
  "FalseGate": {
    "Class": "pasta/FalseConstant",
    "Pos": "{\"x\":60,\"y\":640}",
    "Links": ["output -> [EitherCheck] input 1"],
  },
  "Sum": {
    "Class": "pasta/Sum",
    "Pos": "{\"x\":320,\"y\":80}",
    "Links": [
      "output -> [Difference] input 1",
      "output -> [IsProductLarge] input 2",
      "output -> [IsTotalLessThanProduct] input 1",
      "output -> [Summary] Total",
    ],
  },
  "Product": {
    "Class": "pasta/Mul",
    "Pos": "{\"x\":320,\"y\":220}",
    "Links": [
      "output -> [IsProductLarge] input 1",
      "output -> [IsTotalLessThanProduct] input 2",
    ],
  },
  "Difference": {
    "Class": "pasta/Sub",
    "Pos": "{\"x\":600,\"y\":80}",
    "Links": [
      "output -> [Ratio] input 1",
      "output -> [IsDifferenceNotB] input 1",
      "output -> [Summary] Difference",
    ],
  },
  "Ratio": {
    "Class": "pasta/Div",
    "Pos": "{\"x\":860,\"y\":80}",
    "Links": ["output -> [Summary] Ratio"],
  },
  "IsProductLarge": {
    "Class": "pasta/More",
    "Pos": "{\"x\":600,\"y\":240}",
    "Links": [
      "output -> [AllChecks] input 2",
      "output -> [Summary] Product large",
    ],
  },
  "IsTotalLessThanProduct": {
    "Class": "pasta/Less",
    "Pos": "{\"x\":600,\"y\":380}",
    "Links": [
      "output -> [EitherCheck] input 2",
      "output -> [Summary] Total less",
    ],
  },
  "IsAEqualB": {
    "Class": "pasta/Equal",
    "Pos": "{\"x\":320,\"y\":360}",
    "Links": ["output -> [Summary] Equal"],
  },
  "IsDifferenceNotB": {
    "Class": "pasta/NotEqual",
    "Pos": "{\"x\":860,\"y\":220}",
    "Links": ["output -> [Summary] Different"],
  },
  "AllChecks": {
    "Class": "pasta/BoolAnd",
    "Pos": "{\"x\":860,\"y\":360}",
    "Links": [
      "output -> [SelectedText] Selector",
      "output -> [Summary] All checks",
    ],
  },
  "EitherCheck": {
    "Class": "pasta/BoolOr",
    "Pos": "{\"x\":860,\"y\":500}",
    "Links": [
      "output -> [InvertedEither] input 1",
      "output -> [Summary] Either check",
    ],
  },
  "InvertedEither": {
    "Class": "pasta/BoolNot",
    "Pos": "{\"x\":1120,\"y\":500}",
    "Links": ["output -> [Summary] Inverted"],
  },
  "Greeting": {
    "Class": "pasta/StringConstant",
    "Pos": "{\"x\":60,\"y\":820}",
    "value": " hello,pasta ",
    "Links": ["output -> [Trimmed] input 1"],
  },
  "Separator": {
    "Class": "pasta/StringConstant",
    "Pos": "{\"x\":60,\"y\":960}",
    "value": ",",
    "Links": [
      "output -> [SplitGreeting] Separator",
      "output -> [HasTextSeparator] input 2",
      "output -> [Summary] Separator",
    ],
  },
  "Trimmed": {
    "Class": "pasta/StringTrimSpace",
    "Pos": "{\"x\":320,\"y\":820}",
    "Links": [
      "output -> [Upper] input 1",
      "output -> [Lower] input 1",
      "output -> [SplitGreeting] Text",
      "output -> [HasTextSeparator] input 1",
      "output -> [TextLength] input 1",
    ],
  },
  "Upper": {
    "Class": "pasta/StringUpper",
    "Pos": "{\"x\":600,\"y\":760}",
    "Links": ["output -> [SelectedText] In 1"],
  },
  "Lower": {
    "Class": "pasta/StringLower",
    "Pos": "{\"x\":600,\"y\":900}",
    "Links": ["output -> [SelectedText] In 0"],
  },
  "SplitGreeting": {
    "Class": "pasta/StringSplit",
    "Pos": "{\"x\":600,\"y\":1040}",
    "Links": [
      "Before -> [JoinedText] input 1",
      "After -> [Summary] After",
    ],
  },
  "HasTextSeparator": {
    "Class": "pasta/StringContains",
    "Pos": "{\"x\":600,\"y\":1180}",
    "Links": ["output -> [Summary] Has separator"],
  },
  "TextLength": {
    "Class": "pasta/StringLength",
    "Pos": "{\"x\":600,\"y\":1320}",
    "Links": ["output -> [Summary] Length"],
  },
  "SelectedText": {
    "Class": "pasta/Select",
    "Pos": "{\"x\":860,\"y\":840}",
    "Links": ["Out -> [Summary] Selected"],
  },
  "JoinedText": {
    "Class": "pasta/StringConcat",
    "Pos": "{\"x\":1120,\"y\":920}",
    "Links": ["output -> [Summary] Text"],
  },
  "Summary": {
    "Class": "pasta/StringFormat",
    "Pos": "{\"x\":1400,\"y\":560}",
    "template": [
      {"id": "text-1", "template": "text", "values": {"text": "Summary: "}},
      {"id": "text", "template": "value", "values": {"name": "Text", "type": "pasta/string"}},
      {"id": "text-2", "template": "text", "values": {"text": " | total="}},
      {"id": "total", "template": "value", "values": {"name": "Total", "type": "pasta/int"}},
      {"id": "text-3", "template": "text", "values": {"text": " diff="}},
      {"id": "difference", "template": "value", "values": {"name": "Difference", "type": "pasta/int"}},
      {"id": "text-4", "template": "text", "values": {"text": " ratio="}},
      {"id": "ratio", "template": "value", "values": {"name": "Ratio", "type": "pasta/int"}},
      {"id": "text-5", "template": "text", "values": {"text": " float="}},
      {"id": "float", "template": "value", "values": {"name": "Float", "type": "pasta/float"}},
      {"id": "text-6", "template": "text", "values": {"text": " length="}},
      {"id": "length", "template": "value", "values": {"name": "Length", "type": "pasta/int"}},
      {"id": "text-7", "template": "text", "values": {"text": " selected="}},
      {"id": "selected", "template": "value", "values": {"name": "Selected", "type": "pasta/string"}},
      {"id": "text-8", "template": "text", "values": {"text": " after="}},
      {"id": "after", "template": "value", "values": {"name": "After", "type": "pasta/string"}},
      {"id": "text-9", "template": "text", "values": {"text": " separator="}},
      {"id": "separator-text", "template": "value", "values": {"name": "Separator", "type": "pasta/string"}},
      {"id": "text-10", "template": "text", "values": {"text": " checks="}},
      {"id": "product-large", "template": "value", "values": {"name": "Product large", "type": "pasta/bool"}},
      {"id": "total-less", "template": "value", "values": {"name": "Total less", "type": "pasta/bool"}},
      {"id": "equal", "template": "value", "values": {"name": "Equal", "type": "pasta/bool"}},
      {"id": "different", "template": "value", "values": {"name": "Different", "type": "pasta/bool"}},
      {"id": "separator", "template": "value", "values": {"name": "Has separator", "type": "pasta/bool"}},
      {"id": "all", "template": "value", "values": {"name": "All checks", "type": "pasta/bool"}},
      {"id": "either", "template": "value", "values": {"name": "Either check", "type": "pasta/bool"}},
      {"id": "inverted", "template": "value", "values": {"name": "Inverted", "type": "pasta/bool"}},
    ],
  },
  "Loopback": {
    "Class": "demo.pasta/Loopback",
    "Pos": "{\"x\":1445,\"y\":1643}",
  },
  "OutProxy": {
    "Class": "demo.pasta/OutProxy",
    "Pos": "{\"x\":1445,\"y\":1491}",
  },
  "Client": {
    "Class": "demo.pasta/HttpClient",
    "Pos": "{\"x\":1106,\"y\":1534}",
    "url": "http://127.0.0.1:8080/",
    "method": "GET",
    "Links": ["Network -> [Loopback] Network"],
  },
  "ServerSelector": {
    "Class": "pasta/BoolConstant",
    "Pos": "{\"x\":856,\"y\":1627}",
    "Links": ["output -> [NetSelect] Selector"],
		"value": true,
  },
  "NetSelect": {
    "Class": "pasta/Select",
    "Pos": "{\"x\":1112,\"y\":1734}",
    "Links": ["Out -> [Loopback] Network"],
  },
  "AResponse": {
    "Class": "pasta/StringConstant",
    "Pos": "{\"x\":620,\"y\":1860}",
    "value": "Response from demo server A",
    "Links": ["output -> [ServerA] Response"],
  },
  "BResponse": {
    "Class": "pasta/StringConstant",
    "Pos": "{\"x\":620,\"y\":2000}",
    "value": "Response from demo server B",
    "Links": ["output -> [ServerB] Response"],
  },
  "ServerA": {
    "Class": "demo.pasta/HttpServer",
    "Pos": "{\"x\":860,\"y\":1800}",
    "host": "127.0.0.1",
    "port": 8081,
    "Links": ["Network -> [NetSelect] In 0"],
  },
  "ServerB": {
    "Class": "demo.pasta/HttpServer",
    "Pos": "{\"x\":860,\"y\":1980}",
    "host": "127.0.0.1",
    "port": 8080,
    "Links": ["Network -> [NetSelect] In 1"],
  },
}`

func Classes() []pasta.NodeClass {
	return append(std.StdClasses(), []pasta.NodeClass{
		loopbackClass{},
		httpServerClass{},
		httpClientClass{},
		outproxyClass{},
	}...)
}

func formatHuJSONText(b []byte) (string, error) {
	ast, err := tailscalehujson.Parse(b)
	if err != nil {
		return "", err
	}
	expandHuJSONComposites(&ast)
	ast.Format()
	return string(ast.Pack()), nil
}

func expandHuJSONComposites(v *tailscalehujson.Value) {
	switch value := v.Value.(type) {
	case *tailscalehujson.Object:
		if len(value.Members) > 0 {
			for i := range value.Members {
				value.Members[i].Name.BeforeExtra = ensureHuJSONNewline(value.Members[i].Name.BeforeExtra)
				expandHuJSONComposites(&value.Members[i].Value)
			}
			value.AfterExtra = ensureHuJSONNewline(value.AfterExtra)
		}
	case *tailscalehujson.Array:
		if len(value.Elements) > 0 {
			for i := range value.Elements {
				value.Elements[i].BeforeExtra = ensureHuJSONNewline(value.Elements[i].BeforeExtra)
				expandHuJSONComposites(&value.Elements[i])
			}
			value.AfterExtra = ensureHuJSONNewline(value.AfterExtra)
		}
	}
}

func ensureHuJSONNewline(extra tailscalehujson.Extra) tailscalehujson.Extra {
	if bytes.Contains(extra, []byte("\n")) {
		return extra
	}
	return append(append(tailscalehujson.Extra{}, extra...), '\n')
}
