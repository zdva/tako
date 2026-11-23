package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"

	"github.com/zdva/tako/internal/transport"
)

// Prompts implements [transport.Prompter] with modal dialogs: requests queue
// up from the connecting goroutines and the window shows the oldest one.
type Prompts struct {
	mu    sync.Mutex
	queue []*prompt
	wake  func()
}

type promptKind int

const (
	promptHostKey promptKind = iota
	promptSecret
	promptChallenge
)

// prompt is one pending question. Its fields are touched by the UI goroutine
// only once it is queued, except done, which the asking goroutine reads.
type prompt struct {
	kind promptKind
	hk   transport.HostKeyRequest
	sec  transport.SecretRequest
	ch   transport.ChallengeRequest

	// Form state.
	value    string
	remember bool
	answers  []string

	done chan promptAnswer
}

type promptAnswer struct {
	ok       bool
	value    string
	remember bool
	answers  []string
}

var _ transport.Prompter = (*Prompts)(nil)

// NewPrompts returns an empty queue.
func NewPrompts() *Prompts { return &Prompts{} }

// SetWake sets the function that redraws the window.
func (p *Prompts) SetWake(fn func()) {
	p.mu.Lock()
	p.wake = fn
	p.mu.Unlock()
}

// Pending reports how many requests wait for an answer.
func (p *Prompts) Pending() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.queue)
}

func (p *Prompts) ask(ctx context.Context, q *prompt) (promptAnswer, error) {
	q.done = make(chan promptAnswer, 1)
	p.mu.Lock()
	p.queue = append(p.queue, q)
	wake := p.wake
	p.mu.Unlock()
	if wake != nil {
		wake()
	}
	select {
	case ans := <-q.done:
		return ans, nil
	case <-ctx.Done():
		p.remove(q)
		if wake != nil {
			wake()
		}
		return promptAnswer{}, fmt.Errorf("waiting for the user: %w", ctx.Err())
	}
}

func (p *Prompts) remove(q *prompt) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, x := range p.queue {
		if x == q {
			p.queue = append(p.queue[:i], p.queue[i+1:]...)
			return
		}
	}
}

func (p *Prompts) first() *prompt {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.queue) == 0 {
		return nil
	}
	return p.queue[0]
}

// ConfirmHostKey implements [transport.Prompter].
func (p *Prompts) ConfirmHostKey(ctx context.Context, req transport.HostKeyRequest) (bool, error) {
	ans, err := p.ask(ctx, &prompt{kind: promptHostKey, hk: req})
	return ans.ok, err
}

// Secret implements [transport.Prompter].
func (p *Prompts) Secret(ctx context.Context, req transport.SecretRequest) (transport.SecretReply, error) {
	ans, err := p.ask(ctx, &prompt{kind: promptSecret, sec: req, remember: true})
	if err != nil {
		return transport.SecretReply{}, err
	}
	if !ans.ok {
		return transport.SecretReply{}, transport.ErrCanceled
	}
	return transport.SecretReply{Value: []byte(ans.value), Remember: ans.remember}, nil
}

// Challenge implements [transport.Prompter].
func (p *Prompts) Challenge(ctx context.Context, req transport.ChallengeRequest) ([]string, error) {
	ans, err := p.ask(ctx, &prompt{kind: promptChallenge, ch: req, answers: make([]string, len(req.Questions))})
	if err != nil {
		return nil, err
	}
	if !ans.ok {
		return nil, transport.ErrCanceled
	}
	return ans.answers, nil
}

// CancelAll answers every pending request with a refusal. The app calls it on
// exit so no connecting goroutine stays blocked on a dialog nobody will see.
func (p *Prompts) CancelAll() {
	p.mu.Lock()
	q := p.queue
	p.queue = nil
	p.mu.Unlock()
	for _, x := range q {
		x.done <- promptAnswer{}
	}
}

// answer completes the oldest request.
func (p *Prompts) answer(q *prompt, ans promptAnswer) {
	p.remove(q)
	q.done <- ans
}

// promptView shows the oldest pending request.
func (a *App) promptView(c *ui.Context) {
	if a.d.Prompts == nil {
		return
	}
	q := a.d.Prompts.first()
	if q == nil {
		return
	}
	open := true
	a.dialog(c, &open, func() {
		prompt := ui.Column(c).Key(fmt.Sprintf("prompt:%p", q)).Width(440).Padding(20).Gap(12).Background(a.tk.Surface).
			Radius(22).Border(1, a.tk.BorderStrong)
		a.tk.ShadowLg(prompt)
		prompt.Children(func() {
			switch q.kind {
			case promptHostKey:
				a.hostKeyPrompt(c, q)
			case promptSecret:
				a.secretPrompt(c, q)
			case promptChallenge:
				a.challengePrompt(c, q)
			}
		})
	})
	if !open {
		a.d.Prompts.answer(q, promptAnswer{})
	}
}

func (a *App) hostKeyPrompt(c *ui.Context, q *prompt) {
	ui.Text(c, a.T(kHostKeyTitle, q.hk.Name)).Bold().FontSize(18)
	ui.Text(c, a.T(kHostKeyBody, q.hk.Host)).FontSize(12.5).TextColor(a.tk.Text2)
	ui.Column(c).Padding(10, 12).Gap(3).Radius(10).Background(a.tk.Surface3).Children(func() {
		a.caption(c, q.hk.KeyType)
		ui.Text(c, q.hk.Fingerprint).FontSize(12.5).Bold().Selectable()
	})
	ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
		if ui.Button(c, a.T(kReject)).Clicked() {
			a.d.Prompts.answer(q, promptAnswer{})
		}
		if ui.PrimaryButton(c, a.T(kTrust)).AutoFocus().Clicked() {
			a.d.Prompts.answer(q, promptAnswer{ok: true})
		}
	})
}

func (a *App) secretPrompt(c *ui.Context, q *prompt) {
	title := a.T(kPromptPassword, q.sec.Host.Name)
	if q.sec.Kind == "passphrase" {
		title = a.T(kPromptPassphrase, q.sec.Host.Name)
	}
	ui.Text(c, title).Bold().FontSize(18)
	if q.sec.Detail != "" {
		a.caption(c, q.sec.Detail)
	}
	in := ui.TextInput(c, &q.value).Password().AutoFocus().Label(title)
	ui.Row(c).Gap(8).Children(func() {
		ui.Switch(c, &q.remember).Label(a.T(kRemember))
		ui.Text(c, a.T(kRemember)).FontSize(12)
	})
	submit := func() { a.d.Prompts.answer(q, promptAnswer{ok: true, value: q.value, remember: q.remember}) }
	ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
		if ui.Button(c, a.T(kCancel)).Clicked() {
			a.d.Prompts.answer(q, promptAnswer{})
		}
		if ui.PrimaryButton(c, a.T(kConnect)).Clicked() {
			submit()
		}
	})
	if in.Submitted() {
		submit()
	}
}

func (a *App) challengePrompt(c *ui.Context, q *prompt) {
	title := q.ch.Name
	if strings.TrimSpace(title) == "" {
		title = a.T(kPromptChallenge, q.ch.Host.Name)
	}
	ui.Text(c, title).Bold().FontSize(18)
	if q.ch.Instruction != "" {
		ui.Text(c, q.ch.Instruction).FontSize(12.5).TextColor(a.tk.Text2)
	}
	for i, question := range q.ch.Questions {
		a.field(c, strings.TrimSpace(question), func() {
			in := ui.TextInput(c, &q.answers[i]).Label(question)
			if i == 0 {
				in.AutoFocus()
			}
			if i >= len(q.ch.Echo) || !q.ch.Echo[i] {
				in.Password()
			}
		})
	}
	ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
		if ui.Button(c, a.T(kCancel)).Clicked() {
			a.d.Prompts.answer(q, promptAnswer{})
		}
		if ui.PrimaryButton(c, a.T(kConnect)).Clicked() {
			a.d.Prompts.answer(q, promptAnswer{ok: true, answers: q.answers})
		}
	})
}
