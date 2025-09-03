package main

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

// logRequest prints request details
func logRequest(r *http.Request) {
	log.Printf("%s %s", r.Method, r.URL.String())
	headers, _ := json.MarshalIndent(r.Header, "", "  ")
	log.Printf("Headers: %s", headers)
	if r.ContentLength > 0 {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Error reading body: %v", err)
		} else {
			log.Printf("Body: %s", string(body))
		}
	}
}

func httpHandler(w http.ResponseWriter, r *http.Request) {
	logRequest(r)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func computeAcceptKey(key string) string {
	const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New()
	h.Write([]byte(key + magic))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	logRequest(r)
	if r.Header.Get("Upgrade") != "websocket" {
		http.Error(w, "upgrade required", http.StatusBadRequest)
		return
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "missing websocket key", http.StatusBadRequest)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}
	conn, buf, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, "hijack error", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	accept := computeAcceptKey(key)
	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := conn.Write([]byte(response)); err != nil {
		return
	}

	readLoop(conn, buf.Reader)
}

func readLoop(conn net.Conn, r *bufio.Reader) {
	for {
		op, payload, err := readFrame(r)
		if err != nil {
			if err != io.EOF {
				log.Printf("WS read error: %v", err)
			}
			return
		}
		log.Printf("WS recv (%d): %s", op, string(payload))
		if err := writeFrame(conn, op, payload); err != nil {
			log.Printf("WS write error: %v", err)
			return
		}
	}
}

func readFrame(r *bufio.Reader) (byte, []byte, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	fin := header[0] & 0x80
	op := header[0] & 0x0F
	masked := header[1] & 0x80
	length := int(header[1] & 0x7F)

	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint64(ext[:]))
	}

	var mask [4]byte
	if masked != 0 {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return 0, nil, err
		}
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	if masked != 0 {
		for i := 0; i < length; i++ {
			payload[i] ^= mask[i%4]
		}
	}
	if fin == 0 {
		return 0, nil, io.EOF // fragmented frames not supported
	}
	return op, payload, nil
}

func writeFrame(w io.Writer, op byte, payload []byte) error {
	header := []byte{0x80 | op, 0}
	length := len(payload)
	switch {
	case length < 126:
		header[1] = byte(length)
	case length <= 65535:
		header[1] = 126
		ext := make([]byte, 2)
		binary.BigEndian.PutUint16(ext, uint16(length))
		header = append(header, ext...)
	default:
		header[1] = 127
		ext := make([]byte, 8)
		binary.BigEndian.PutUint64(ext, uint64(length))
		header = append(header, ext...)
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	if length > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsHandler)
	mux.HandleFunc("/", httpHandler)

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("Server listening on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
