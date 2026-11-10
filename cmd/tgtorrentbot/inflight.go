package main

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/commands"
)

// inFlightChats records the chats of updates from allowed users that are
// being handled. The webhook acknowledges an update before handling it, so
// Telegram never redelivers one a shutdown cuts off; shutdown tells these
// chats instead (interruptedText), so the user does not wait for a reply
// that will never come.
type inFlightChats struct {
	mu    sync.Mutex
	next  int
	chats map[int]int64
}

// track records chatID until the returned function is called.
func (f *inFlightChats) track(chatID int64) (done func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.chats == nil {
		f.chats = map[int]int64{}
	}
	id := f.next
	f.next++
	f.chats[id] = chatID
	return func() {
		f.mu.Lock()
		delete(f.chats, id)
		f.mu.Unlock()
	}
}

// list returns the distinct chats with an update still being handled.
func (f *inFlightChats) list() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []int64
	for _, c := range f.chats {
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}

const interruptedText = "The bot restarted before it finished your request. Please send it again."

// noticeBudget is the part of the shutdown timeout kept for telling chats
// that their request was interrupted.
const noticeBudget = 2 * time.Second

// tellInterrupted sends interruptedText to every chat, concurrently, within
// ctx.
func tellInterrupted(ctx context.Context, api *telegram.API, chats []int64) {
	var wg sync.WaitGroup
	for _, chatID := range chats {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = commands.SendMessage(ctx, api, telegram.SendMessageParams{ChatID: chatID, Text: interruptedText})
		}()
	}
	wg.Wait()
	logger.Warn("Shutdown cut off updates still being handled; told %d chat(s) to send them again", len(chats))
}
