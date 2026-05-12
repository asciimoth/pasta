package main

import (
	"testing"

	"github.com/asciimoth/pasta/pasta"
	"github.com/asciimoth/pasta/pasta/pastatest"
)

func TestStringLibraryConformance(t *testing.T) {
	pastatest.RunSuite(t, pastatest.Suite{
		LibraryName: StringLibraryName,
		NewLibrary:  func(*testing.T) pasta.Library { return StringLibrary{} },
		Classes:     StringClasses(),
		Links: []pastatest.LinkCase{
			{
				Name:   "text to trim",
				Input:  pastatest.Endpoint{Class: TrimClass, Port: StringInput},
				Output: pastatest.Endpoint{Class: TextClass, Port: StringOutput},
				Type:   StringType,
			},
			{
				Name:   "split rest to result",
				Input:  pastatest.Endpoint{Class: StringResultClass, Port: StringInput},
				Output: pastatest.Endpoint{Class: SplitClass, Port: StringRestOutput},
				Type:   StringType,
			},
		},
	})
}

func TestStreamLibraryConformance(t *testing.T) {
	pastatest.RunSuite(t, pastatest.Suite{
		LibraryName: StreamLibraryName,
		NewLibrary:  func(*testing.T) pasta.Library { return StreamLibrary{} },
		Classes:     StreamClasses(),
		Links: []pastatest.LinkCase{
			{
				Name:   "sink pulls uppercase",
				Input:  pastatest.Endpoint{Class: StreamUppercaseClass, Port: StreamInput},
				Output: pastatest.Endpoint{Class: StreamSinkClass, Port: StreamOutput},
				Type:   StreamType,
			},
			{
				Name:   "prefix pulls provider",
				Input:  pastatest.Endpoint{Class: StreamProviderClass, Port: StreamInput},
				Output: pastatest.Endpoint{Class: StreamPrefixClass, Port: StreamOutput},
				Type:   StreamType,
			},
		},
	})
}
