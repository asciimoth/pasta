// Package pastatest provides reusable conformance suites and helpers for
// libraries built on github.com/asciimoth/pasta/pasta.
//
// The helpers are intended for downstream node, class, and library
// implementations. A package can describe its library and representative link
// scenarios, then call RunSuite from a normal Go test:
//
//	func TestPastaConformance(t *testing.T) {
//		pastatest.RunSuite(t, pastatest.StaticSuite(
//			"example.com",
//			[]pasta.ClassSpec{sourceClass, sinkClass},
//			[]pastatest.LinkCase{{
//				Name:   "source to sink",
//				Input:  pastatest.Endpoint{Class: "example.com/Sink", Port: pastatest.Input(1)},
//				Output: pastatest.Endpoint{Class: "example.com/Source", Port: pastatest.Output(1)},
//				Type:   "example.com/value",
//			}},
//		))
//	}
package pastatest
