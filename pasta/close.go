package pasta

import "io"

func CloseBackground(c io.Closer) {
	go func() {
		_ = c.Close()
	}()
}
