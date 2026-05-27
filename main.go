package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	_ "embed"
	"encoding/pem"
	"errors"
	"log"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"sync"
	"syscall"
	"time"
)

//go:embed replies/initservices.json
var initServicesReply []byte

var (
	initServicesPath = regexp.MustCompile(`^/rest/sdp/v[\d.]+/initservices$`)
	noticePath       = regexp.MustCompile(`^/rest/sdp/v[\d.]+/notice$`)
	eulaPath         = regexp.MustCompile(`^/rest/sdp/v[\d.]+/eula$`)
	serverStatusPath = regexp.MustCompile(`^/rest/apps/[^/]+/serverstatus/status$`)
)

func handler(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s %s %s", r.RemoteAddr, r.Method, r.URL.Path)

	switch {
	case r.Method == http.MethodPost && initServicesPath.MatchString(r.URL.Path):
		w.Header().Set("X-Server-Time", strconv.FormatInt(time.Now().UnixMilli(), 10))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(initServicesReply)

	case r.Method == http.MethodGet && noticePath.MatchString(r.URL.Path):
		writeJSON(w, `{"notices":[]}`)

	case r.Method == http.MethodGet && eulaPath.MatchString(r.URL.Path):
		writeJSON(w, `{"eula":[]}`)

	case r.Method == http.MethodGet && serverStatusPath.MatchString(r.URL.Path):
		writeJSON(w, `{"status":"ok"}`)

	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

// TV cannot validate certs at cold boot — no trusted clock — so a fresh
// self-signed cert with the right SANs is accepted.
func newSelfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "lgtvsdp.com"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{
			"*.lgtvsdp.com",
			"*.lge.com",
			"*.lgsmartad.com",
			"*.lgappstv.com",
		},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	cert, err := newSelfSignedCert()
	if err != nil {
		log.Fatalf("generate cert: %v", err)
	}

	mux := http.HandlerFunc(handler)

	httpSrv := &http.Server{
		Addr:              envOr("LGTV_HTTP_ADDR", ":80"),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	httpsSrv := &http.Server{
		Addr:              envOr("LGTV_HTTPS_ADDR", ":443"),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
		},
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		log.Printf("HTTP  listening on %s", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		log.Printf("HTTPS listening on %s", httpsSrv.Addr)
		if err := httpsSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("https: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Print("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
	_ = httpsSrv.Shutdown(ctx)
	wg.Wait()
}
