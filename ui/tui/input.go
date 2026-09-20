package tui

import (
	"io"
	"os"
	"time"
	"unicode/utf8"
)

// keyReader 提供带超时的单字节读取，用于按键解析。
// get(timeout<=0) 阻塞等待；get(timeout>0) 至多等待 timeout，超时返回 (0,false)。
type keyReader struct {
	get func(timeout time.Duration) (byte, bool)
}

// newTTYKeyReader 启动 goroutine 从 os.Stdin 读原始字节推入通道。
// raw 模式下 Read 按字节/突发返回；单消费者（主循环）串行消费。
func newTTYKeyReader() *keyReader {
	ch := make(chan byte, 64)
	go func() {
		buf := make([]byte, 128)
		for {
			n, err := os.Stdin.Read(buf)
			for i := 0; i < n; i++ {
				ch <- buf[i]
			}
			if err != nil {
				close(ch)
				return
			}
		}
	}()
	return &keyReader{
		get: func(timeout time.Duration) (byte, bool) {
			if timeout <= 0 {
				b, ok := <-ch
				return b, ok
			}
			select {
			case b, ok := <-ch:
				return b, ok
			case <-time.After(timeout):
				return 0, false
			}
		},
	}
}

// newBytesKeyReader 供测试注入字节流；timeout 被忽略，数据耗尽即返回 false。
func newBytesKeyReader(data []byte) *keyReader {
	pos := 0
	return &keyReader{
		get: func(timeout time.Duration) (byte, bool) {
			if pos >= len(data) {
				return 0, false
			}
			b := data[pos]
			pos++
			return b, true
		},
	}
}

// readRune 阻塞读一个完整 rune（含多字节 UTF-8）。
func (kr *keyReader) readRune() (rune, error) {
	var buf [utf8.UTFMax]byte
	for i := 0; i < utf8.UTFMax; i++ {
		b, ok := kr.get(0)
		if !ok {
			if i == 0 {
				return 0, io.EOF
			}
			return 0, io.ErrUnexpectedEOF
		}
		buf[i] = b
		if utf8.FullRune(buf[:i+1]) {
			r, _ := utf8.DecodeRune(buf[:i+1])
			return r, nil
		}
	}
	return utf8.RuneError, nil
}
