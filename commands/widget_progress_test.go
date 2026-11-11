package commands

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
	"github.com/odwrtw/transmission"
)

const testHash = "0123456789abcdef0123456789abcdef01234567"

// Telegram refuses a widget over 32768 bytes or 4096 bytes after zlib at
// level 9 (INPUT_MEDIA_WIDGET_TOO_LARGE).
func TestProgressWidgetFitsTelegramLimits(t *testing.T) {
	if n := len(progressWidgetHTML); n == 0 || n > 32768 {
		t.Fatalf("widget HTML is %d bytes", n)
	}
	var buf bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	zw.Write([]byte(progressWidgetHTML))
	zw.Close()
	if buf.Len() > 4096 {
		t.Fatalf("widget HTML compresses to %d bytes, limit 4096", buf.Len())
	}
	t.Logf("widget: %d bytes, %d compressed", len(progressWidgetHTML), buf.Len())
}

func TestWidgetStateFitsTelegramLimit(t *testing.T) {
	tr := &transmission.Torrent{
		HashString:   testHash,
		Name:         strings.Repeat("Ж", 2000),
		ErrorString:  strings.Repeat("e", 2000),
		SizeWhenDone: 1 << 40, PercentDone: 0.5, RateDownload: 1 << 30, Eta: 1 << 20,
		Status: transmission.StatusDownloading,
		Labels: []string{"1", "audiobooks", "1"},
	}
	if n := len(torrentWidgetState(tr).json()); n > 1024 {
		t.Fatalf("state is %d bytes, limit 1024", n)
	}
}

func TestTorrentWidgetStatus(t *testing.T) {
	cases := []struct {
		status int
		done   float64
		want   string
	}{
		{transmission.StatusDownloading, 0.3, widgetDownloading},
		{transmission.StatusStopped, 0.3, widgetStopped},
		{transmission.StatusDownloadPending, 0, widgetQueued},
		{transmission.StatusChecking, 0.1, widgetChecking},
		{transmission.StatusSeeding, 1, widgetDone},
		{transmission.StatusStopped, 1, widgetDone},
	}
	for _, c := range cases {
		st := torrentWidgetState(&transmission.Torrent{HashString: testHash, Status: c.status, PercentDone: c.done, Eta: -1})
		if st.Status != c.want || st.ETA != 0 && c.want != widgetDownloading {
			t.Errorf("status %d done %v: got %+v, want %s", c.status, c.done, st, c.want)
		}
	}
}

func TestWidgetProgressAccepts(t *testing.T) {
	for data, want := range map[string]bool{
		"p:" + testHash:                  true,
		"p:" + strings.ToUpper(testHash): true,
		"p:" + testHash[:39]:             false,
		"p:" + testHash + "0":            false,
		"x:" + testHash:                  false,
	} {
		ok, _ := (&WidgetProgressCommandFactory{}).Accepts(&telegram.Update{WidgetQuery: &telegram.WidgetQuery{Data: data}})
		if ok != want {
			t.Errorf("Accepts(%q) = %v, want %v", data, ok, want)
		}
	}
	if ok, _ := (&WidgetProgressCommandFactory{}).Accepts(&telegram.Update{Message: &telegram.Message{Text: "p:" + testHash}}); ok {
		t.Error("a text message must not be taken for a widget query")
	}
}

// recordingTelegram answers every method with ok and records calls with
// their JSON bodies.
type recordingTg struct {
	mu    sync.Mutex
	calls []string
	body  map[string]map[string]any
}

func newRecordingTg(t *testing.T, sendWidgetReply string) (*recordingTg, *telegram.API) {
	rec := &recordingTg{body: map[string]map[string]any{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		rec.mu.Lock()
		rec.calls = append(rec.calls, method)
		rec.body[method] = body
		rec.mu.Unlock()
		if method == "sendWidget" && sendWidgetReply != "" {
			w.Write([]byte(sendWidgetReply))
			return
		}
		w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":1}}}`))
	}))
	t.Cleanup(srv.Close)
	return rec, telegram.NewAPI("tok", telegram.WithBaseURL(srv.URL))
}

func (r *recordingTg) snapshot() ([]string, map[string]map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...), r.body
}

// fakeTransmission serves torrent-get with torrents and accepts the rest.
func fakeTransmission(t *testing.T, torrents string) *transmission.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		switch {
		case bytes.Contains(b, []byte(`"torrent-get"`)):
			w.Write([]byte(`{"result":"success","arguments":{"torrents":` + torrents + `}}`))
		case bytes.Contains(b, []byte(`"torrent-add"`)):
			w.Write([]byte(`{"result":"success","arguments":{"torrent-added":{"id":3,"name":"X","hashString":"` + testHash + `"}}}`))
		default:
			w.Write([]byte(`{"result":"success","arguments":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	c, err := transmission.New(transmission.Config{Address: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func widgetQueryUpdate() *telegram.Update {
	return &telegram.Update{WidgetQuery: &telegram.WidgetQuery{
		ID: "wq", From: &telegram.User{ID: 7}, Chat: &telegram.Chat{ID: 7, Type: "private"},
		MessageID: 55, Data: "p:" + testHash,
	}}
}

func TestWidgetQueryAnswersProgress(t *testing.T) {
	rec, api := newRecordingTg(t, "")
	tc := fakeTransmission(t, `[{"id":3,"name":"The Wire","hashString":"`+strings.ToUpper(testHash)+`","percentDone":0.62,"status":4,"rateDownload":8000000,"eta":540,"sizeWhenDone":1000,"labels":["7","shows","7"]}]`)
	cmd := &WidgetProgressCommand{Hash: testHash, Env: environment.Env{TransmissionClient: tc, TgApi: api}}

	if err := cmd.Handle(context.Background(), widgetQueryUpdate()); err != nil {
		t.Fatal(err)
	}
	calls, body := rec.snapshot()
	if len(calls) != 1 || calls[0] != "answerWidgetQuery" {
		t.Fatalf("calls = %v, want only answerWidgetQuery (no state edit while downloading)", calls)
	}
	var st widgetState
	if err := json.Unmarshal([]byte(body["answerWidgetQuery"]["data"].(string)), &st); err != nil {
		t.Fatal(err)
	}
	want := widgetState{Hash: testHash, Name: "The Wire", Category: "TV Shows", Size: 1000, Done: 0.62, Status: widgetDownloading, Rate: 8000000, ETA: 540}
	if st != want {
		t.Fatalf("answer = %+v, want %+v", st, want)
	}
}

func TestWidgetQueryStoresFinalState(t *testing.T) {
	for name, torrents := range map[string]string{
		"done": `[{"id":3,"name":"The Wire","hashString":"` + testHash + `","percentDone":1,"status":6,"labels":["7","shows","7"]}]`,
		"gone": `[]`,
	} {
		t.Run(name, func(t *testing.T) {
			rec, api := newRecordingTg(t, "")
			cmd := &WidgetProgressCommand{Hash: testHash, Env: environment.Env{TransmissionClient: fakeTransmission(t, torrents), TgApi: api}}
			if err := cmd.Handle(context.Background(), widgetQueryUpdate()); err != nil {
				t.Fatal(err)
			}
			calls, body := rec.snapshot()
			if strings.Join(calls, ",") != "answerWidgetQuery,editMessageWidgetData" {
				t.Fatalf("calls = %v", calls)
			}
			edit := body["editMessageWidgetData"]
			if edit["chat_id"] != float64(7) || edit["message_id"] != float64(55) || edit["chat_data"] != body["answerWidgetQuery"]["data"] {
				t.Fatalf("edit = %v, answer = %v", edit, body["answerWidgetQuery"])
			}
			if !strings.Contains(edit["chat_data"].(string), `"s":"`+name+`"`) {
				t.Fatalf("chat_data = %v", edit["chat_data"])
			}
		})
	}
}

func TestPrivateDownloadSendsWidget(t *testing.T) {
	rec, api := newRecordingTg(t, "")
	cmd := &DownloadMagnetWithCategoryCommand{
		Env:       environment.Env{TransmissionClient: fakeTransmission(t, `[]`), TgApi: api, DownloadPath: t.TempDir()},
		MagnetURI: "magnet:?xt=urn:btih:abc",
		Category:  CategoryShows,
	}
	upd := &telegram.Update{CallbackQuery: &telegram.CallbackQuery{
		ID: "q", From: &telegram.User{ID: 7}, Message: &telegram.Message{Chat: telegram.Chat{ID: 7}},
	}}
	if err := cmd.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	calls, body := rec.snapshot()
	if strings.Contains(strings.Join(calls, ","), "sendMessage") || body["sendWidget"] == nil {
		t.Fatalf("calls = %v, want a widget and no text", calls)
	}
	w := body["sendWidget"]
	if w["allow_queries"] != true || w["html"] != progressWidgetHTML || !strings.Contains(w["chat_data"].(string), testHash) {
		t.Fatalf("sendWidget = %v", w)
	}
	if w["caption"] != "Added 3: X [TV Shows]" {
		t.Fatalf("caption = %q", w["caption"])
	}
}

func TestWidgetRefusedFallsBackToText(t *testing.T) {
	rec, api := newRecordingTg(t, `{"ok":false,"error_code":400,"description":"Bad Request: method not found"}`)
	cmd := &DownloadMagnetWithCategoryCommand{
		Env:       environment.Env{TransmissionClient: fakeTransmission(t, `[]`), TgApi: api, DownloadPath: t.TempDir()},
		MagnetURI: "magnet:?xt=urn:btih:abc",
		Category:  CategoryShows,
	}
	upd := &telegram.Update{CallbackQuery: &telegram.CallbackQuery{
		ID: "q", From: &telegram.User{ID: 7}, Message: &telegram.Message{Chat: telegram.Chat{ID: 7}},
	}}
	if err := cmd.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if calls, body := rec.snapshot(); body["sendMessage"] == nil {
		t.Fatalf("calls = %v, want the text message after the refused widget", calls)
	}
}
