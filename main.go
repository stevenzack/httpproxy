package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func init() {
	log.SetFlags(log.Lshortfile)
}
func main() {
	httpsProxy(":8080")
}
func httpsProxy(addr string) {
	l, e := net.Listen("tcp", addr)
	if e != nil {
		log.Panic(e)
		return
	}
	for {
		c, e := l.Accept()
		if e != nil {
			log.Panic(e)
			return
		}
		go handleHTTP(c)
	}
}

func handleHTTP(conn net.Conn) {
	defer conn.Close()
	c, ok := conn.(*net.TCPConn)
	if !ok {
		panic("the conn is not *net.TCPConn")
	}
	r, e := readReq(c)
	if e != nil {
		log.Println(e)
		return
	}
	rbuf := r.Buffer()
	if r.Method == http.MethodConnect {
		//tunnel https
		e = tunnel(r, c)
		if e != nil {
			log.Println(e)
			return
		}
		return
	}

	//http proxy
	a, e := net.Dial("tcp", r.GetHost())
	if e != nil {
		log.Println(e)
		return
	}
	defer a.Close()

	_, e = a.Write(rbuf.Bytes())
	if e != nil {
		log.Println(e)
		return
	}

	buf := make([]byte, 4<<10)
	for {
		n, e := a.Read(buf)
		if e != nil {
			if e == io.EOF {
				if n > 0 {
					_, e = c.Write(buf[:n])
					if e != nil {
						log.Println(e)
						return
					}
				}
				break
			}
			log.Println(e)
			return
		}
		_, e = c.Write(buf[:n])
		if e != nil {
			log.Println(e)
			return
		}
	}
}
func tunnel(r *Request, c *net.TCPConn) error {
	println("tunnel.. ", r.Headers.Get("Host"))
	a, e := net.Dial("tcp", r.Headers.Get("Host"))
	if e != nil {
		log.Println(e)
		return e
	}
	defer a.Close()
	rp := NewResponse(200)
	_, e = rp.WriteTo(c)
	if e != nil {
		log.Println(e)
		return e
	}

	go func() {
		buf := make([]byte, 4<<20)
		total := 0
		// cache := new(bytes.Buffer)
		for {
			n, e := a.Read(buf)
			if e != nil {
				log.Println(e)
				return
			}
			if n == 0 {
				continue
			}
			total += n
			fmt.Print("\n----- w n=", n, " -----\n", sanitizeString(buf[:n]))
			// cache.Write(buf[:n])
			// if total < 6000 {
			// 	continue
			// }
			// time.Sleep(time.Second)
			_, e = c.Write(buf[:n])
			if e != nil {
				log.Println(e)
				return
			}
		}
	}()
	buf := make([]byte, 4<<20)
	for {
		n, e := c.Read(buf)
		if e != nil {
			log.Println(e)
			return e
		}
		if n == 0 {
			continue
		}
		fmt.Print("\n----- r -----\n", sanitizeString(buf[:n]))

		_, e = a.Write(buf[:n])
		if e != nil {
			log.Println(e)
			return e
		}
	}
}

func sanitizeString(b []byte) string {
	// o, _ := json.Marshal(string(b))
	// return string(o)
	if len(b) < 100 {
		return fmt.Sprint(b)
	}
	return fmt.Sprint(b)
}

type Request struct {
	Method        string
	RequestURI    string
	URL           *url.URL
	Protocol      string
	Headers       http.Header
	ContentLength int64
	Body          *bytes.Buffer
}

func (r *Request) GetHost() string {
	s := r.Headers.Get("Host")
	if !strings.Contains(s, ":") {
		s += ":80"
	}
	return s
}
func (r *Request) Buffer() *bytes.Buffer {
	if _, ok := r.Headers["Content-Length"]; !ok {
		l := 0
		if r.Body != nil && r.Body.Len() > 0 {
			l = r.Body.Len()
		}
		r.Headers.Set("Content-Length", strconv.Itoa(l))
	}
	buf := new(bytes.Buffer)
	buf.WriteString(r.Method)
	buf.WriteString(" ")
	buf.WriteString(r.RequestURI)
	buf.WriteString(" ")
	buf.WriteString(r.Protocol)
	buf.WriteString("\r\n")
	for k := range r.Headers {
		buf.WriteString(k)
		buf.WriteString(": ")
		buf.WriteString(r.Headers.Get(k))
		buf.WriteString("\r\n")
	}
	buf.WriteString("\r\n")

	if r.Body != nil && r.Body.Len() > 0 {
		buf.Write(r.Body.Bytes())
	}
	return buf
}

type Response struct {
	Protocol   string
	StatusCode int
	Headers    http.Header
	Body       *bytes.Buffer
}

func NewResponse(code int) *Response {
	return &Response{
		Protocol:   "HTTP/1.1",
		StatusCode: code,
		Headers:    make(http.Header),
		Body:       new(bytes.Buffer),
	}
}

/* HTTP/1.1 200 OK
 */
func (r *Response) WriteTo(w io.Writer) (int64, error) {
	if _, ok := r.Headers["Content-Length"]; !ok {
		l := 0
		if r.Body != nil && r.Body.Len() > 0 {
			l = r.Body.Len()
		}
		r.Headers.Set("Content-Length", strconv.Itoa(l))
	}
	buf := new(bytes.Buffer)
	buf.WriteString(r.Protocol)
	buf.WriteString(" ")
	buf.WriteString(strconv.Itoa(r.StatusCode))
	buf.WriteString(" ")
	buf.WriteString(http.StatusText(r.StatusCode))
	buf.WriteString("\r\n")
	if r.Headers != nil && len(r.Headers) > 0 {
		for k := range r.Headers {
			buf.WriteString(k)
			buf.WriteString(": ")
			buf.WriteString(r.Headers.Get(k))
			buf.WriteString("\r\n")
		}
	}
	buf.WriteString("\r\n")

	w.Write(buf.Bytes())
	//body
	if r.Body != nil && r.Body.Len() > 0 {
		_, e := w.Write(r.Body.Bytes())
		if e != nil {
			log.Println(e)
			return 0, e
		}
	}
	w.Write([]byte("\r\n\r\n"))
	return 0, nil
}

type WindowQueue struct {
	Data [4]byte
	l    uint8
}

func (d *WindowQueue) Push(v byte) (byte, bool) {
	first := d.Data[0]
	l := int(d.l)
	// append
	if l < len(d.Data) {
		d.Data[l] = v
		d.l++
		return 0, false
	}

	//pop
	for i := range d.Data {
		if i == len(d.Data)-1 {
			d.Data[i] = v
			break
		}
		d.Data[i] = d.Data[i+1]
	}

	return first, true
}
func (d *WindowQueue) Len() int {
	return int(d.l)
}
func (d *WindowQueue) String() string {
	return string(d.Data[:d.Len()])
}
func (d *WindowQueue) Reset() {
	d.l = 0
	for i := range d.Data {
		d.Data[i] = 0
	}
}

func ValidateHttpMethod(s string) bool {
	switch s {
	case http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodConnect,
		http.MethodOptions,
		http.MethodTrace:
		return true
	default:
		return false
	}
}

/*
Proxy:
GET http://google.com/ HTTP/1.1
Normal:
GET /about HTTP/1.1
*/
func readReq(c io.Reader) (*Request, error) {
	var r = new(Request)
	r.Headers = make(http.Header)
	e := read(c, func(line string) (int64, error) {
		if r.Method == "" {
			ss := strings.Split(line, " ")
			if len(ss) < 3 {
				return 0, fmt.Errorf("invalid Method line:%s", line)
			}
			r.Method = ss[0]
			if !ValidateHttpMethod(r.Method) {
				return 0, fmt.Errorf("invalid http method:%s", r.Method)
			}
			r.RequestURI = ss[1]
			var e error
			r.URL, e = url.Parse(r.RequestURI)
			if e != nil {
				return 0, fmt.Errorf("invalid requestURI: %s", r.RequestURI)
			}
			r.Protocol = ss[2]
			return 0, nil
		}
		ss := strings.SplitN(line, ":", 2)
		if len(ss) < 2 {
			return 0, fmt.Errorf("invalid header line:%s", line)
		}
		k := strings.TrimSpace(ss[0])
		v := strings.TrimSpace(ss[1])
		r.Headers.Add(k, v)
		if k == "Content-Length" {
			var e error
			r.ContentLength, e = strconv.ParseInt(v, 10, 64)
			if e != nil {
				return 0, fmt.Errorf("invalid content-length: %s", line)
			}
		}
		return r.ContentLength, nil
	}, func(b []byte) {
		if r.Body == nil {
			r.Body = new(bytes.Buffer)
		}
		r.Body.Write(b)
	})
	if e != nil {
		log.Println(e)
		return nil, e
	}

	return r, nil
}

func read(c io.Reader, handleLine func(line string) (int64, error), handleBody func(b []byte)) error {
	line := new(strings.Builder)
	var contentLength int64
	window := new(WindowQueue)
	buf := make([]byte, 37)
	neverRead := true
	for {
		n, e := c.Read(buf)
		if e != nil {
			log.Println(e)
			return e
		}
		if n == 0 {
			return nil
		}

		if neverRead && buf[0] < '0' {
			if buf[0] == 22 {
				return fmt.Errorf("you seem to request HTTPS on a HTTP server")
			}
			return fmt.Errorf("the request message is not in English")
		}
		neverRead = false
		//handle
		for i := 0; i < n; i++ {
			v := buf[i]
			pop, ok := window.Push(v)
			if !ok {
				if window.String() == "\r\n" {
					if contentLength <= 0 {
						return nil
					}
					// read body
					e := readBody(c, contentLength, handleBody)
					if e != nil {
						log.Println(e)
						return e
					}

					return nil
				}
				continue
			}

			line.WriteByte(pop)
			if v != '\n' || window.Data[len(window.Data)-2] != '\r' {
				continue
			}

			// header end
			if window.String() == "\r\n\r\n" {
				if contentLength <= 0 {
					return nil
				}
				// read body
				e := readBody(c, contentLength, handleBody)
				if e != nil {
					log.Println(e)
					return e
				}

				return nil
			}

			// header
			line.Write(window.Data[:len(window.Data)-2])
			//parse line
			s := strings.TrimLeft(line.String(), "\r\n")
			s = strings.TrimSpace(s)
			contentLength, e = handleLine(s)
			if e != nil {
				log.Println(e)
				return e
			}

			line.Reset()
			window.Reset()
		}

		if len(buf) > n {
			break
		}
	}
	return nil
}

func readBody(c io.Reader, contentLength int64, handleBody func(b []byte)) error {
	buf := make([]byte, 37)
	var read int64 = 0
	for {
		n, e := c.Read(buf)
		if e != nil {
			if e == io.EOF {
				if n > 0 {
					goto NORMAL
				}
				return nil
			}
			log.Println(e)
			return e
		}
		if n == 0 {
			return nil
		}
	NORMAL:
		if read+int64(n) > contentLength {
			n -= int(read + int64(n) - contentLength)
			handleBody(buf[:n])
			return nil
		}
		if read+int64(n) == contentLength {
			handleBody(buf[:n])
			return nil
		}
		handleBody(buf[:n])
		read += int64(n)
	}
}
