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
		"Pos":   "{\"x\":60,\"y\":80}",
		"Links": [
			"output -> [IsAEqualB] input 1",
			"output -> [Product] input 1",
			"output -> [Ratio] input 2",
			"output -> [Sum] input 1",
		],
		"value": "7",
	},
	"B": {
		"Class": "pasta/IntConstant",
		"Pos":   "{\"x\":60,\"y\":220}",
		"Links": [
			"output -> [Difference] input 2",
			"output -> [IsAEqualB] input 2",
			"output -> [IsDifferenceNotB] input 2",
			"output -> [Product] input 2",
			"output -> [Sum] input 2",
		],
		"value": "5",
	},
	"FloatValue": {
		"Class": "pasta/FloatConstant",
		"Pos":   "{\"x\":60,\"y\":360}",
		"Links": [
			"output -> [Summary] Float",
		],
		"value": "2.5",
	},
	"TrueGate": {
		"Class": "pasta/TrueConstant",
		"Pos":   "{\"x\":60,\"y\":520}",
		"Links": [
			"output -> [AllChecks] input 1",
		],
		"value": true,
	},
	"FalseGate": {
		"Class": "pasta/FalseConstant",
		"Pos":   "{\"x\":60,\"y\":640}",
		"Links": [
			"output -> [EitherCheck] input 1",
		],
		"value": false,
	},
	"Sum": {
		"Class": "pasta/Sum",
		"Pos":   "{\"x\":320,\"y\":80}",
		"Links": [
			"output -> [Difference] input 1",
			"output -> [IsProductLarge] input 2",
			"output -> [IsTotalLessThanProduct] input 1",
			"output -> [Summary] Total",
		],
		"Primary": "pasta/int",
	},
	"Product": {
		"Class": "pasta/Mul",
		"Pos":   "{\"x\":320,\"y\":220}",
		"Links": [
			"output -> [IsProductLarge] input 1",
			"output -> [IsTotalLessThanProduct] input 2",
		],
		"Primary": "pasta/int",
	},
	"Difference": {
		"Class": "pasta/Sub",
		"Pos":   "{\"x\":600,\"y\":80}",
		"Links": [
			"output -> [IsDifferenceNotB] input 1",
			"output -> [Ratio] input 1",
			"output -> [Summary] Difference",
		],
		"Primary": "pasta/int",
	},
	"Ratio": {
		"Class": "pasta/Div",
		"Pos":   "{\"x\":860,\"y\":80}",
		"Links": [
			"output -> [Summary] Ratio",
		],
		"Primary": "pasta/int",
	},
	"IsProductLarge": {
		"Class": "pasta/More",
		"Pos":   "{\"x\":600,\"y\":240}",
		"Links": [
			"output -> [AllChecks] input 2",
			"output -> [Summary] Product large",
		],
	},
	"IsTotalLessThanProduct": {
		"Class": "pasta/Less",
		"Pos":   "{\"x\":600,\"y\":380}",
		"Links": [
			"output -> [EitherCheck] input 2",
			"output -> [Summary] Total less",
		],
	},
	"IsAEqualB": {
		"Class": "pasta/Equal",
		"Pos":   "{\"x\":320,\"y\":360}",
		"Links": [
			"output -> [Summary] Equal",
		],
	},
	"IsDifferenceNotB": {
		"Class": "pasta/NotEqual",
		"Pos":   "{\"x\":860,\"y\":220}",
		"Links": [
			"output -> [Summary] Different",
		],
	},
	"AllChecks": {
		"Class": "pasta/BoolAnd",
		"Pos":   "{\"x\":860,\"y\":360}",
		"Links": [
			"output -> [SelectedText] Selector",
			"output -> [Summary] All checks",
		],
	},
	"EitherCheck": {
		"Class": "pasta/BoolOr",
		"Pos":   "{\"x\":860,\"y\":500}",
		"Links": [
			"output -> [InvertedEither] input 1",
			"output -> [Summary] Either check",
		],
	},
	"InvertedEither": {
		"Class": "pasta/BoolNot",
		"Pos":   "{\"x\":1120,\"y\":500}",
		"Links": [
			"output -> [Summary] Inverted",
		],
	},
	"Greeting": {
		"Class": "pasta/StringConstant",
		"Pos":   "{\"x\":60,\"y\":820}",
		"Links": [
			"output -> [Trimmed] input 1",
		],
		"value": " hello,pasta ",
	},
	"Separator": {
		"Class": "pasta/StringConstant",
		"Pos":   "{\"x\":60,\"y\":960}",
		"Links": [
			"output -> [HasTextSeparator] input 2",
			"output -> [SplitGreeting] Separator",
			"output -> [Summary] Separator",
		],
		"value": ",",
	},
	"Trimmed": {
		"Class": "pasta/StringTrimSpace",
		"Pos":   "{\"x\":320,\"y\":820}",
		"Links": [
			"output -> [HasTextSeparator] input 1",
			"output -> [Lower] input 1",
			"output -> [SplitGreeting] Text",
			"output -> [TextLength] input 1",
			"output -> [Upper] input 1",
		],
	},
	"Upper": {
		"Class": "pasta/StringUpper",
		"Pos":   "{\"x\":600,\"y\":760}",
		"Links": [
			"output -> [SelectedText] In 1",
		],
	},
	"Lower": {
		"Class": "pasta/StringLower",
		"Pos":   "{\"x\":600,\"y\":900}",
		"Links": [
			"output -> [SelectedText] In 0",
		],
	},
	"SplitGreeting": {
		"Class": "pasta/StringSplit",
		"Pos":   "{\"x\":600,\"y\":1040}",
		"Links": [
			"Before -> [JoinedText] input 1",
			"After -> [Summary] After",
		],
	},
	"HasTextSeparator": {
		"Class": "pasta/StringContains",
		"Pos":   "{\"x\":600,\"y\":1180}",
		"Links": [
			"output -> [Summary] Has separator",
		],
	},
	"TextLength": {
		"Class": "pasta/StringLength",
		"Pos":   "{\"x\":600,\"y\":1320}",
		"Links": [
			"output -> [Summary] Length",
		],
	},
	"SelectedText": {
		"Class": "pasta/Select",
		"Pos":   "{\"x\":860,\"y\":840}",
		"Links": [
			"Out -> [Summary] Selected",
		],
		"Primary": "pasta/string",
	},
	"JoinedText": {
		"Class": "pasta/StringConcat",
		"Pos":   "{\"x\":1120,\"y\":920}",
		"Links": [
			"output -> [Summary] Text",
		],
	},
	"Summary": {
		"Class": "pasta/StringFormat",
		"Pos":   "{\"x\":1400,\"y\":560}",
		"template": [
			{
				"id":       "text-1",
				"template": "text",
				"values": {
					"text": "Summary: ",
				},
			},
			{
				"id":       "text",
				"template": "value",
				"values": {
					"name": "Text",
					"type": "pasta/string",
				},
			},
			{
				"id":       "text-2",
				"template": "text",
				"values": {
					"text": " | total=",
				},
			},
			{
				"id":       "total",
				"template": "value",
				"values": {
					"name": "Total",
					"type": "pasta/int",
				},
			},
			{
				"id":       "text-3",
				"template": "text",
				"values": {
					"text": " diff=",
				},
			},
			{
				"id":       "difference",
				"template": "value",
				"values": {
					"name": "Difference",
					"type": "pasta/int",
				},
			},
			{
				"id":       "text-4",
				"template": "text",
				"values": {
					"text": " ratio=",
				},
			},
			{
				"id":       "ratio",
				"template": "value",
				"values": {
					"name": "Ratio",
					"type": "pasta/int",
				},
			},
			{
				"id":       "text-5",
				"template": "text",
				"values": {
					"text": " float=",
				},
			},
			{
				"id":       "float",
				"template": "value",
				"values": {
					"name": "Float",
					"type": "pasta/float",
				},
			},
			{
				"id":       "text-6",
				"template": "text",
				"values": {
					"text": " length=",
				},
			},
			{
				"id":       "length",
				"template": "value",
				"values": {
					"name": "Length",
					"type": "pasta/int",
				},
			},
			{
				"id":       "text-7",
				"template": "text",
				"values": {
					"text": " selected=",
				},
			},
			{
				"id":       "selected",
				"template": "value",
				"values": {
					"name": "Selected",
					"type": "pasta/string",
				},
			},
			{
				"id":       "text-8",
				"template": "text",
				"values": {
					"text": " after=",
				},
			},
			{
				"id":       "after",
				"template": "value",
				"values": {
					"name": "After",
					"type": "pasta/string",
				},
			},
			{
				"id":       "text-9",
				"template": "text",
				"values": {
					"text": " separator=",
				},
			},
			{
				"id":       "separator-text",
				"template": "value",
				"values": {
					"name": "Separator",
					"type": "pasta/string",
				},
			},
			{
				"id":       "text-10",
				"template": "text",
				"values": {
					"text": " checks=",
				},
			},
			{
				"id":       "product-large",
				"template": "value",
				"values": {
					"name": "Product large",
					"type": "pasta/bool",
				},
			},
			{
				"id":       "total-less",
				"template": "value",
				"values": {
					"name": "Total less",
					"type": "pasta/bool",
				},
			},
			{
				"id":       "equal",
				"template": "value",
				"values": {
					"name": "Equal",
					"type": "pasta/bool",
				},
			},
			{
				"id":       "different",
				"template": "value",
				"values": {
					"name": "Different",
					"type": "pasta/bool",
				},
			},
			{
				"id":       "separator",
				"template": "value",
				"values": {
					"name": "Has separator",
					"type": "pasta/bool",
				},
			},
			{
				"id":       "all",
				"template": "value",
				"values": {
					"name": "All checks",
					"type": "pasta/bool",
				},
			},
			{
				"id":       "either",
				"template": "value",
				"values": {
					"name": "Either check",
					"type": "pasta/bool",
				},
			},
			{
				"id":       "inverted",
				"template": "value",
				"values": {
					"name": "Inverted",
					"type": "pasta/bool",
				},
			},
		],
	},
	"Loopback": {
		"Class": "demo.pasta/Loopback",
		"Pos":   "{\"x\":1600,\"y\":1638}",
	},
	"OutProxy": {
		"Class": "demo.pasta/OutProxy",
		"Pos":   "{\"x\":1555,\"y\":1441}",
		"url":   "socks5+ws://localhost:1080",
	},
	"Proxy URL": {
		"Class": "pasta/StringConstant",
		"Pos":   "{\"x\":1274,\"y\":1411}",
		"Links": [
			"output -> [OutProxy] URL",
		],
		"value": "socks5+ws://localhost:1080",
	},
	"Client": {
		"Class": "demo.pasta/HttpClient",
		"Pos":   "{\"x\":1169,\"y\":1530}",
		"Links": [
			"Network -> [SelectOut 438] In",
		],
		"url":    "http://127.0.0.1:8080/",
		"method": "GET",
		"body":   "",
	},
	"Client URL": {
		"Class": "pasta/StringConstant",
		"Pos":   "{\"x\":764,\"y\":1528}",
		"value": "http://example.com/",
		"Links": [
			"output -> [Select 470] In 0",
		],
	},
	"Client URL A": {
		"Class": "pasta/StringConstant",
		"Pos":   "{\"x\":523,\"y\":1560}",
		"value": "http://127.0.0.1:8081/",
		"Links": [
			"output -> [Select 460] In 0",
		],
	},
	"Client URL B": {
		"Class": "pasta/StringConstant",
		"Pos":   "{\"x\":533,\"y\":1656}",
		"Links": [
			"output -> [Select 460] In 1",
		],
		"value": "http://127.0.0.1:8080/",
	},
	"NetSelect": {
		"Class": "pasta/Select",
		"Pos":   "{\"x\":1165,\"y\":1780}",
		"Links": [
			"Out -> [Loopback] Network",
		],
		"Primary": "demo.pasta/network",
	},
	"AResponse": {
		"Class": "pasta/StringConstant",
		"Pos":   "{\"x\":536,\"y\":1761}",
		"Links": [
			"output -> [ServerA] Response",
		],
		"value": "Response from demo server A",
	},
	"BResponse": {
		"Class": "pasta/StringConstant",
		"Pos":   "{\"x\":567,\"y\":2054}",
		"Links": [
			"output -> [ServerB] Response",
		],
		"value": "Response from demo server B",
	},
	"Host": {
		"Class": "pasta/StringConstant",
		"Pos":   "{\"x\":616,\"y\":1852}",
		"Links": [
			"output -> [ServerA] Host",
		],
		"value": "localhost",
	},
	"Port": {
		"Class": "pasta/StringConstant",
		"Pos":   "{\"x\":619,\"y\":1950}",
		"Links": [
			"output -> [ServerA] Port",
		],
		"value": "8081",
	},
	"ServerA": {
		"Class": "demo.pasta/HttpServer",
		"Pos":   "{\"x\":860,\"y\":1800}",
		"Links": [
			"Network -> [NetSelect] In 0",
		],
		"host":     "localhost",
		"port":     8081,
		"response": "Response from demo server A",
	},
	"ServerB": {
		"Class": "demo.pasta/HttpServer",
		"Pos":   "{\"x\":860,\"y\":1980}",
		"Links": [
			"Network -> [NetSelect] In 1",
		],
		"host":     "127.0.0.1",
		"port":     8080,
		"response": "Response from demo server B",
	},
	"SelectOut 438": {
		"Class":   "pasta/SelectOut",
		"Primary": "demo.pasta/network",
		"Pos":     "{\"x\":1350,\"y\":1530}",
		"Links": [
			"Out 0 -> [OutProxy] Network",
			"Out 1 -> [Loopback] Network",
		],
	},
	"BoolConstant 447": {
		"value": true,
		"Class": "pasta/BoolConstant",
		"Pos":   "{\"x\":807,\"y\":1414}",
		"Links": [
			"output -> [SelectOut 438] Selector",
			"output -> [Select 470] Selector",
		],
	},
	"Select 460": {
		"Class":   "pasta/Select",
		"Primary": "pasta/string",
		"Pos":     "{\"x\":815,\"y\":1636}",
		"Links": [
			"Out -> [Select 470] In 1",
		],
	},
	"BoolConstant 467": {
		"value": true,
		"Class": "pasta/BoolConstant",
		"Pos":   "{\"x\":431,\"y\":1878}",
		"Links": [
			"output -> [Select 460] Selector",
			"output -> [NetSelect] Selector",
		],
	},
	"Select 470": {
		"Class":   "pasta/Select",
		"Primary": "pasta/string",
		"Pos":     "{\"x\":1006,\"y\":1530}",
		"Links": [
			"Out -> [Client] URL",
		],
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
