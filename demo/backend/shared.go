package main

import (
	"bytes"

	"github.com/asciimoth/pasta/pasta"
	"github.com/asciimoth/pasta/pasta/std"
	tailscalehujson "github.com/tailscale/hujson"
)

const initialConfig = `{
	"A": {
		"Class": "pasta/IntConstant",
		// Positions are frontend-owned JSON strings. Pasta preserves them.
		"Pos":   "{\"x\":60,\"y\":80}",
		"Links": [
			"output -> [IsAEqualB] input 1",
			"output -> [Product] input 1",
			"output -> [Ratio] input 2",
			"output -> [Sum] input 1",
			"output -> [Var Int Set] Value",
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
			"output -> [Var Float Set] Value",
		],
		"value": "2.5",
	},
	"TrueGate": {
		"Class": "pasta/TrueConstant",
		"Pos":   "{\"x\":60,\"y\":520}",
		"Links": [
			"output -> [AllChecks] input 1",
			"output -> [Var Bool Set] Value",
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
			"output -> [Var String Set] Value",
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
			"Out -> [ObjectPack] In Selected Tag",
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
	"ObjectName": {
		"Class": "pasta/StringConstant",
		"Pos":   "{\"x\":1830,\"y\":511}",
		"Links": [
			"output -> [ObjectPack] In Name",
		],
		"value": "demo-object",
	},
	"ObjectCount": {
		"Class": "pasta/IntConstant",
		"Pos":   "{\"x\":1829,\"y\":621}",
		"Links": [
			"output -> [ObjectPack] In Count",
		],
		"value": "3",
	},
	"ObjectBase": {
		"Class": "pasta/ObjectConstant",
		"Pos":   "{\"x\":1856,\"y\":424}",
		"Links": [
			"output -> [ObjectPack] Base",
		],
		"value": {
			"payload": {
				"source": "base",
				"count": 1,
				"legacy": true,
				"tags": [
					"base-tag",
				],
			},
			"keep": true,
			"drop": {
				"reason": "removed by ObjectPack delete_paths",
			},
		},
	},
	"ObjectConstant": {
		"Class": "pasta/ObjectConstant",
		"Pos":   "{\"x\":1687,\"y\":1055}",
		"Links": [
			"output -> [ObjectPack] In Raw",
			"output -> [Var Object Set] Value",
		],
		"value": {
			"source":  "constant",
			"enabled": true,
		},
	},
	"ObjectPack": {
		"Class": "pasta/ObjectPacker",
		"Pos":   "{\"x\":2115,\"y\":520}",
		"Links": [
			"output -> [ObjectUnpack] input",
			"output -> [ObjectString] input",
		],
		"root": "map",
		"fields": [
			{
				"id":   "name",
				"name": "Name",
				"type": "pasta/string",
				"path": ["payload", "name"],
			},
			{
				"id":   "count",
				"name": "Count",
				"type": "pasta/int",
				"path": ["payload", "count"],
			},
			{
				"id":   "raw",
				"name": "Raw",
				"type": "pasta/object",
				"path": ["raw"],
			},
			{
				"id":        "selected-tag",
				"name":      "Selected Tag",
				"type":      "pasta/string",
				"path":      ["payload", "tags"],
				"operation": "append",
			},
		],
		"containers": [
			{
				"id":   "payload",
				"path": ["payload"],
				"kind": "map",
			},
		],
		"delete_paths": [
			{
				"id":   "payload-source",
				"path": ["payload", "source"],
			},
			{
				"id":   "legacy",
				"path": ["payload", "legacy"],
			},
			{
				"id":   "drop",
				"path": ["drop"],
			},
			{
				"id":   "keep",
				"path": ["keep"],
			},
		],
	},
	"ObjectUnpack": {
		"Class": "pasta/ObjectUnpacker",
		"Pos":   "{\"x\":2330,\"y\":474}",
		"Links": [
			"Out Count -> [ObjectSummary] Count",
			"Out Name -> [ObjectSummary] Name",
		],
		"outputs": [
			{
				"id":      "name",
				"name":    "Name",
				"type":    "pasta/string",
				"path":    ["payload", "name"],
				"default": "missing",
			},
			{
				"id":      "count",
				"name":    "Count",
				"type":    "pasta/int",
				"path":    ["payload", "count"],
				"default": 0,
			},
			{
				"id":   "raw",
				"name": "Raw",
				"type": "pasta/object",
				"path": ["raw"],
			},
		],
	},
	"ObjectString": {
		"Class":      "pasta/ObjectToString",
		"Pos":        "{\"x\":2341,\"y\":603}",
		"pretty":     false,
		"omit_empty": false,
		"Links": [
			"output -> [ObjectSummary] JSON",
		],
	},
	"ObjectSummary": {
		"Class": "pasta/StringFormat",
		"Pos":   "{\"x\":2543,\"y\":504}",
		"template": [
			{
				"id":       "text-1",
				"template": "text",
				"values": {
					"text": "Object: ",
				},
			},
			{
				"id":       "object-name",
				"template": "value",
				"values": {
					"name": "Name",
					"type": "pasta/string",
				},
			},
			{
				"id":       "text-2",
				"template": "text",
				"values": {
					"text": " count=",
				},
			},
			{
				"id":       "object-count",
				"template": "value",
				"values": {
					"name": "Count",
					"type": "pasta/int",
				},
			},
			{
				"id":       "text-3",
				"template": "text",
				"values": {
					"text": " json=",
				},
			},
			{
				"id":       "object-json",
				"template": "value",
				"values": {
					"name": "JSON",
					"type": "pasta/string",
				},
			},
		],
	},
	// Config on top of node blocks become popups
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
	"Manual Trigger": {
		"Class": "pasta/Trigger",
		"Pos":   "{\"x\":1866,\"y\":1301}",
		"Links": [
			"Trigger -> [Var Bool Set] Trigger",
			"Trigger -> [Var Float Set] Trigger",
			"Trigger -> [Var Int Set] Trigger",
			"Trigger -> [Var Object Set] Trigger",
			"Trigger -> [Var String Set] Trigger",
		],
	},
	"Var Int Set": {
		"Class": "pasta/IntSet",
		"Pos":   "{\"x\":2324,\"y\":763}",
		"Links": [
			"Trigger -> [Var Int Get] Trigger",
		],
		"name":  "demo/int",
		"value": 7,
	},
	"Var Int Get": {
		"Class": "pasta/IntGet",
		"Pos":   "{\"x\":2561,\"y\":759}",
		"Links": [
			"Value -> [Variable Summary] Int",
		],
		"name": "demo/int",
	},
	"Var Float Set": {
		"Class": "pasta/FloatSet",
		"Pos":   "{\"x\":2322,\"y\":898}",
		"Links": [
			"Trigger -> [Var Float Get] Trigger",
		],
		"name":  "demo/float",
		"value": 2.5,
	},
	"Var Float Get": {
		"Class": "pasta/FloatGet",
		"Pos":   "{\"x\":2565,\"y\":898}",
		"Links": [
			"Value -> [Variable Summary] Float",
		],
		"name": "demo/float",
	},
	"Var String Set": {
		"Class": "pasta/StringSet",
		"Pos":   "{\"x\":2322,\"y\":1030}",
		"Links": [
			"Trigger -> [Var String Get] Trigger",
		],
		"name":  "demo/string",
		"value": " hello,pasta ",
	},
	"Var String Get": {
		"Class": "pasta/StringGet",
		"Pos":   "{\"x\":2561,\"y\":1031}",
		"Links": [
			"Value -> [Variable Summary] String",
		],
		"name": "demo/string",
	},
	"Var Bool Set": {
		"Class": "pasta/BoolSet",
		"Pos":   "{\"x\":2324,\"y\":1162}",
		"Links": [
			"Trigger -> [Var Bool Get] Trigger",
		],
		"name":  "demo/bool",
		"value": true,
	},
	"Var Bool Get": {
		"Class": "pasta/BoolGet",
		"Pos":   "{\"x\":2563,\"y\":1162}",
		"Links": [
			"Value -> [Variable Summary] Bool",
		],
		"name": "demo/bool",
	},
	"Var Object Set": {
		"Class": "pasta/ObjectSet",
		"Pos":   "{\"x\":2325,\"y\":1296}",
		"Links": [
			"Trigger -> [Var Object Get] Trigger",
		],
		"name": "demo/object",
		"value": {
			"source":  "constant",
			"enabled": true,
		},
	},
	"Var Object Get": {
		"Class": "pasta/ObjectGet",
		"Pos":   "{\"x\":2563,\"y\":1299}",
		"Links": [
			"Value -> [Variable Object String] input",
		],
		"name": "demo/object",
	},
	"Variable Object String": {
		"Class":      "pasta/ObjectToString",
		"Pos":        "{\"x\":2744,\"y\":1316}",
		"pretty":     false,
		"omit_empty": false,
		"Links": [
			"output -> [Variable Summary] Object JSON",
		],
	},
	"Variable Summary": {
		"Class": "pasta/StringFormat",
		"Pos":   "{\"x\":2964,\"y\":991}",
		"template": [
			{
				"id":       "text-1",
				"template": "text",
				"values": {
					"text": "Variables: int=",
				},
			},
			{
				"id":       "var-int",
				"template": "value",
				"values": {
					"name": "Int",
					"type": "pasta/int",
				},
			},
			{
				"id":       "text-2",
				"template": "text",
				"values": {
					"text": " float=",
				},
			},
			{
				"id":       "var-float",
				"template": "value",
				"values": {
					"name": "Float",
					"type": "pasta/float",
				},
			},
			{
				"id":       "text-3",
				"template": "text",
				"values": {
					"text": " string=",
				},
			},
			{
				"id":       "var-string",
				"template": "value",
				"values": {
					"name": "String",
					"type": "pasta/string",
				},
			},
			{
				"id":       "text-4",
				"template": "text",
				"values": {
					"text": " bool=",
				},
			},
			{
				"id":       "var-bool",
				"template": "value",
				"values": {
					"name": "Bool",
					"type": "pasta/bool",
				},
			},
			{
				"id":       "text-5",
				"template": "text",
				"values": {
					"text": " object=",
				},
			},
			{
				"id":       "var-object",
				"template": "value",
				"values": {
					"name": "Object JSON",
					"type": "pasta/string",
				},
			},
		],
	},
	"Trigger Gateway": {
		"Class": "pasta/Gateway",
		"Pos":   "{\"x\":1462,\"y\":1217}",
	},
	"Trigger Popup": {
		"Class": "pasta/PopUp",
		"Pos":   "{\"x\":1662,\"y\":1153}",
	},
	"Popup Demo": {
		"Class": "demo.pasta/PopupDemo",
		"Pos":   "{\"x\":1650,\"y\":1320}",
	},
}`

func Classes() []pasta.NodeClass {
	return append(std.StdClasses(), []pasta.NodeClass{
		loopbackClass{},
		httpServerClass{},
		httpClientClass{},
		outproxyClass{},
		popupDemoClass{},
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
