package email

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSMTPTLSConfigVerifiesCertificates(t *testing.T) {
	c, err := smtpTLSConfig("smtp.example.org")
	if err != nil {
		t.Fatal(err)
	}
	if c.InsecureSkipVerify {
		t.Fatal("certificate verification must stay on")
	}
	if c.ServerName != "smtp.example.org" || c.MinVersion < tls.VersionTLS12 {
		t.Fatalf("unexpected config: %+v", c)
	}
}

// A server with a certificate nobody trusts must be refused before any credentials are sent.
func TestSendRefusesAnUntrustedCertificate(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{"localhost"}, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan bool, 1) // the server completed a TLS handshake with the client
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second)) // never hang a client that wrongly accepts
		r := bufio.NewReader(conn)
		conn.Write([]byte("220 test\r\n"))
		r.ReadString('\n') // EHLO
		conn.Write([]byte("250-test\r\n250 STARTTLS\r\n"))
		r.ReadString('\n') // STARTTLS
		conn.Write([]byte("220 go ahead\r\n"))
		tc := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
		accepted <- tc.Handshake() == nil
	}()

	_, port, _ := net.SplitHostPort(ln.Addr().String())
	p, _ := strconv.Atoi(port)
	err = NewSMTPProvider("localhost", p, "user", "secret", "from@example.org").Send("to@example.org", "s", "b")
	if err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Fatalf("want a TLS error, got %v", err)
	}
	select {
	case ok := <-accepted:
		if ok {
			t.Fatal("the client completed TLS with an untrusted certificate")
		}
	case <-time.After(5 * time.Second):
	}
}
