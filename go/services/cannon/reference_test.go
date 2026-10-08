package cannon

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestCapturedHaskellHTTP(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/go-migration/services/cannon/oracle/http-reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var records []struct {
		ID           string `json:"id"`
		Method, Path string
		Headers      map[string]string
		RequestBody  *string `json:"request_body"`
		Status       int
		ContentType  *string `json:"content_type"`
		Allow        *string
		ResponseBody any `json:"response_body"`
	}
	if err = json.Unmarshal(raw, &records); err != nil {
		t.Fatal(err)
	}
	app := NewServer(Config{})
	for _, r := range records {
		t.Run(r.ID, func(t *testing.T) {
			var body []byte
			if r.RequestBody != nil {
				body = []byte(*r.RequestBody)
			}
			request := httptest.NewRequest(r.Method, r.Path, bytes.NewReader(body))
			for k, v := range r.Headers {
				request.Header.Set(k, v)
			}
			response := httptest.NewRecorder()
			app.ServeHTTP(response, request)
			if response.Code != r.Status {
				t.Fatalf("status %d want %d", response.Code, r.Status)
			}
			for key, want := range map[string]*string{"Content-Type": r.ContentType, "Allow": r.Allow} {
				actual := response.Header().Get(key)
				if want == nil && actual != "" || want != nil && actual != *want {
					t.Fatalf("%s %q want %v", key, actual, want)
				}
			}
			var actual any
			if err = json.Unmarshal(response.Body.Bytes(), &actual); err != nil {
				actual = response.Body.String()
			}
			got, _ := json.Marshal(actual)
			want, _ := json.Marshal(r.ResponseBody)
			if !bytes.Equal(got, want) {
				t.Fatalf("body %s want %s", got, want)
			}
		})
	}
}
func TestClassicReplacementAndRawPush(t *testing.T) {
	var registrations atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { registrations.Add(1); w.WriteHeader(201) }))
	defer provider.Close()
	app := NewServer(Config{})
	app.client = provider.Client()
	app.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = "http"
		r.URL.Host = provider.Listener.Addr().String()
		return http.DefaultTransport.RoundTrip(r)
	})
	host := "127.0.0.1"
	app.Config.Cannon.ExternalHost = &host
	server := httptest.NewServer(app)
	defer server.Close()
	headers := http.Header{"Z-User": {"11111111-1111-4111-8111-111111111111"}, "Z-Connection": {"fixture"}, "Origin": {"https://synthetic.example.invalid"}}
	dial := func(path string) *websocket.Conn {
		conn, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:]+path, headers)
		if err != nil {
			t.Fatal(err)
		}
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		return conn
	}
	old := dial("/await?client=abcd")
	defer old.Close()
	newer := dial("/websocket?client=abcd")
	defer newer.Close()
	deadline := time.Now().Add(time.Second)
	for registrations.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if registrations.Load() != 2 {
		t.Fatal("presence registrations missing")
	}
	old.Close()
	time.Sleep(10 * time.Millisecond)
	raw := []byte(" {\"raw\": [1, 2], \"synthetic\": true} ")
	request, _ := http.NewRequest("POST", server.URL+"/i/push/11111111-1111-4111-8111-111111111111/fixture", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	kind, data, err := newer.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if kind != websocket.BinaryMessage || !bytes.Equal(data, raw) {
		t.Fatalf("raw binary push changed: %d %q", kind, data)
	}
	if err = newer.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	kind, data, err = newer.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage || string(data) != "pong" {
		t.Fatalf("pong mismatch: %d %q %v", kind, data, err)
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBulkValidatesEntireRequestBeforeEffectsAndKeepsNumbers(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201) }))
	defer provider.Close()
	app := NewServer(Config{})
	host := "127.0.0.1"
	app.Config.Cannon.ExternalHost = &host
	app.client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = "http"
		r.URL.Host = provider.Listener.Addr().String()
		return http.DefaultTransport.RoundTrip(r)
	})}
	server := httptest.NewServer(app)
	defer server.Close()
	headers := http.Header{"Z-User": {"11111111-1111-4111-8111-111111111111"}, "Z-Connection": {"fixture"}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:]+"/await", headers)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Ensure the registration/read loop is running before exercising push.
	conn.WriteMessage(websocket.TextMessage, []byte("ping"))
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err = conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	item := `{"notification":{"id":"22222222-2222-4222-8222-222222222222","payload":[{"large":9223372036854775809}]},"targets":[{"user_id":"11111111-1111-4111-8111-111111111111","conn_id":"fixture"}]}`
	post := func(body string) int {
		response, err := http.Post(server.URL+"/i/bulkpush", "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		return response.StatusCode
	}
	if status := post(`{"bulkpush_req":[` + item + `]}`); status != 200 {
		t.Fatal(status)
	}
	kind, raw, err := conn.ReadMessage()
	if err != nil || kind != 2 || !bytes.Contains(raw, []byte("9223372036854775809")) {
		t.Fatalf("number lost: %d %s %v", kind, raw, err)
	}
	if status := post(`{"bulkpush_req":[` + item + `,{"notification":null,"targets":[]}]}`); status != 400 {
		t.Fatal(status)
	}
	conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, raw, err = conn.ReadMessage(); err == nil {
		t.Fatalf("invalid request caused push: %s", raw)
	}
}
