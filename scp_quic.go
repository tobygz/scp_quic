package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/big"
	"os"
	"path"
	"strings"
	"time"

	quic "github.com/lucas-clemente/quic-go"
)

func decompress(in bytes.Buffer) string {
	var out bytes.Buffer
	r, _ := zlib.NewReader(&in)
	io.Copy(&out, r)
	return out.String()
}

func compress(input []byte) []byte {
	var in bytes.Buffer
	w := zlib.NewWriter(&in)
	w.Write(input)
	w.Close()
	return in.Bytes()
}

func main() {
	bserv := flag.Bool("s", false, "is server")
	caddr := flag.String("a", "10.80.1.140:4200", "addri+port")
	parentAddr := flag.String("p", "/home/devops/upgrade_stats/quic_upload/", "point to the dest path")
	abpath := flag.Bool("d", false, "use abstract path when true; other use relative path")
	// for cli mode
	filename := flag.String("f", "vos_web.exe", "filename")
	filelist := flag.String("flist", "", "filelist")

	flag.Parse()
	fmt.Printf("bserv: %v caddr: %v, parentaddr: %v abpath: %v filename: %v\n",
		*bserv, *caddr, *parentAddr, *abpath, *filename)
	if *bserv {
		fmt.Println("is server, addr:", *caddr)
		go func() {
			defer func() {
				if err := recover(); err != nil {
					fmt.Println("recover exception:", err)
				}
				fmt.Println("c")
			}()
			log.Fatal(echoServer(*caddr, *parentAddr))
		}()

		ch := make(chan int, 1)
		<-ch
	} else {
		fmt.Println("is client :", *filename)
		if len(*filename) == 0 {
			panic("need filename ")
		}
		err := clientMain(*caddr, *filename, *abpath, *filelist)
		if err != nil {
			panic(err)
		}
	}

}

// Start a server that echos all data on the first stream opened by the client
func echoServer(caddr, fulldir string) error {
	listener, err := quic.ListenAddr(caddr, generateTLSConfig(), nil)
	if err != nil {
		return err
	}
	for {
		sess, err := listener.Accept(context.Background())
		if err != nil {
			return err
		}
		go func(se quic.Session) {
			stream, err := se.AcceptStream(context.Background())
			if err != nil {
				panic(err)
			}
			fmt.Println("get remote connected")
			for {
				//read fname len
				lenb := make([]byte, 4)
				io.ReadFull(stream, lenb)
				lenv := binary.LittleEndian.Uint32(lenb)
				//read fname
				fname := make([]byte, lenv)
				io.ReadFull(stream, fname)
				fmt.Println("get fname: ", fname)

				//read content
				io.ReadFull(stream, lenb)
				lenv = binary.LittleEndian.Uint32(lenb)
				fmt.Println("get content len: ", lenv)

				// /home/yuandan/quic-go/quic-go/example/scp/test
				//fulldir := "/home/yuandan/quic-go/quic-go/example/scp/test/"
				//fulldir := "/home/devops/upgrade_stats/quic_upload/"

				fullurl := fmt.Sprintf("%s%s_%d", fulldir, string(fname), now())
				sfname := string(fname)
				if strings.Contains(sfname, "/") {
					//mkdir fulldir+"/"+dirname(fname)
					dir, file := path.Split(sfname)
					if dir == "." {
						dir = fulldir
						fullurl = fmt.Sprintf("%s%s", fulldir, file)
					} else {
						dir = fmt.Sprintf("%s%s", fulldir, dir)
						fullurl = fmt.Sprintf("%s/%s", dir, file)
					}
					os.MkdirAll(dir, os.ModePerm)
				}

				file, ee := os.OpenFile(fullurl, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0600)
				if ee != nil {
					panic(ee)
				}
				if err != nil {
					panic(fmt.Sprintf("err open:%v", err))
				}
				var out bytes.Buffer
				buf := make([]byte, 1024)
				acc := uint32(0)
				for {
					n, err := io.ReadAtLeast(stream, buf, 1)
					if err != nil {
						panic(err)
					}
					out.Write(buf[:n])
					acc += uint32(n)
					fmt.Printf("got acc: %d/%d\n", acc, lenv)
					if acc >= lenv {
						fmt.Println("send end")
						stream.Write([]byte("END"))
						break
					}
				}
				deOut := decompress(out)
				file.WriteString(deOut)
				file.Close()

				//get echo
				ctrl := make([]byte, 1)
				_, err = io.ReadFull(stream, ctrl)
				if err != nil {
					panic(err)
				}
				fmt.Printf("got finished file: %s\n", fullurl)
				fmt.Printf("got finished ctrl: %v\n", ctrl[0])
				if ctrl[0] != byte(0) {
					fmt.Println("conn quit")
					break
				}
			}

		}(sess)
	}

	// Echo through the loggingWriter
	//_, err = io.Copy(loggingWriter{stream}, stream)
	return err
}

func now() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

func clientSendOne(stream quic.Stream, file string, abstract bool) {
	//send filename
	lenv := make([]byte, 4)
	if !abstract {
		ary := strings.Split(file, "/")
		fname := ary[len(ary)-1]
		binary.LittleEndian.PutUint32(lenv, uint32(len(fname)))
		stream.Write(lenv)
		stream.Write([]byte(fname))
	} else {
		binary.LittleEndian.PutUint32(lenv, uint32(len(file)))
		stream.Write(lenv)
		stream.Write([]byte(file))
	}

	fmt.Println("file:", file)
	//send file content
	fs, err := os.Open(file)
	if err != nil {
		panic(err)
	}
	defer fs.Close()
	ocontent, err := ioutil.ReadAll(fs)
	if err != nil {
		panic(err)
	}

	content := compress(ocontent)

	binary.LittleEndian.PutUint32(lenv, uint32(len(content)))
	stream.Write(lenv)

	var offset int = 0
	var span int = 1024
	start := now()
	needBreak := false
	for {
		end := offset + span
		if end > len(content) {
			end = len(content)
			needBreak = true
		}
		if len(content) > 1024*100 {
			fmt.Printf("Client: Sending '%d/%d' %f%%\n", end, len(content), float64(end)*float64(100)/float64(len(content)))
		}
		_, err = stream.Write(content[offset:end])
		if err != nil {
			return
		}
		offset = end
		if needBreak {
			break
		}
	}
	total := now() - start
	avg := float64(len(content)/1024/1024) / (float64(total) / 1000)
	fmt.Printf("use %d msec send %fM, avg: %f (M/sec)\r\n", total, float64(len(content))/float64(1024*1024), avg)
	waitEcho(stream)
	fmt.Println("get echo")
}

func waitEcho(stream quic.Stream) {
	//buf := []byte{byte('E'), byte('N'), byte('D')}
	buf := make([]byte, 3)
	_, err := io.ReadFull(stream, buf)
	if err != nil {
		panic(err)
	}
}

func clientMain(caddr string, file string, abstract bool, filelist string) error {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-echo-example"},
	}
	session, err := quic.DialAddr(caddr, tlsConf, nil)
	if err != nil {
		return err
	}

	stream, err := session.OpenStreamSync(context.Background())
	if err != nil {
		return err
	}
	if filelist == "" {
		clientSendOne(stream, file, abstract)
		stream.Write([]byte{byte(1)})
	} else {
		//read one by one from flist
		file, err := os.Open(filelist)
		if err != nil {
			panic(err)
		}
		defer file.Close()
		br := bufio.NewReader(file)
		count := 0
		for {
			line, _, e := br.ReadLine()
			if e == io.EOF {
				if count != 0 {
					stream.Write([]byte{byte(1)})
				}
				break
			}
			if count != 0 {
				stream.Write([]byte{byte(0)})
			}
			clientSendOne(stream, string(line), abstract)
			count++
		}
	}
	time.Sleep(time.Second)

	return nil
}

// A wrapper for io.Writer that also logs the message.
type loggingWriter struct{ io.Writer }

func (w loggingWriter) Write(b []byte) (int, error) {
	fmt.Printf("Server: Got '%d'\n", len(b))
	return w.Writer.Write(b)
}

// Setup a bare-bones TLS config for the server
func generateTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"quic-echo-example"},
	}
}
