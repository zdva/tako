package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/zdva/tako/internal/hostlib"
	"github.com/zdva/tako/internal/model"
	"github.com/zdva/tako/internal/secrets"
)

// Auth choices, in the order of the segmented control.
const (
	authPassword = iota
	authKey
	authAgent
	authKI
)

// hostSheet is the Add/Edit host dialog.
type hostSheet struct {
	open    bool
	editing model.ID // empty when adding

	name, addr, port, user string
	tags                   []string
	newTag                 string
	os, auth               int
	keyPath, secret        string
	remember               bool
	showSecret             bool
	jump                   []model.ID
	jumpPick               string
	proxy                  int // 0 none, 1 socks5, 2 http
	proxyAddr, proxyUser   string
	proxyPass              string
	keepAlive              string
	startDir, startCmd     string
	policy                 int

	// rest holds the auth entries of an edited host that the form does not
	// show (imported fallback chains); they are kept on save.
	rest         []model.Auth
	existingAuth model.Auth

	testing   bool
	testSeq   int
	testOK    string
	testErr   string
	saveErr   string
	importMsg string
}

func (s *hostSheet) init(*App) { s.remember = true }

func (a *App) openAddHost() {
	s := &a.sheet
	*s = hostSheet{open: true, remember: true, port: "22", os: 0}
}

func (a *App) openEditHost(h model.Host) {
	s := &a.sheet
	*s = hostSheet{
		open: true, editing: h.ID, remember: true,
		name: h.Name, addr: h.Addr, user: h.User, tags: slices.Clone(h.Tags),
		os: hostMatchesOS(h.OS), startDir: h.StartDir, startCmd: h.StartCmd,
		jump: hopsToIDs(h.Jump),
	}
	if h.Port != 0 {
		s.port = strconv.Itoa(h.Port)
	} else {
		s.port = "22"
	}
	if h.KeepAlive != 0 {
		s.keepAlive = strconv.Itoa(h.KeepAlive)
	}
	if h.HostKeyPolicy == model.HostKeyStrict {
		s.policy = 1
	}
	if len(h.Auth) > 0 {
		s.existingAuth = h.Auth[0]
		s.rest = slices.Clone(h.Auth[1:])
		switch h.Auth[0].Kind {
		case model.AuthPassword:
			s.auth = authPassword
		case model.AuthKey:
			s.auth, s.keyPath = authKey, h.Auth[0].KeyPath
		case model.AuthAgent:
			s.auth = authAgent
		case model.AuthKeyboardInteractive:
			s.auth = authKI
		}
	}
	if h.Proxy != nil {
		s.proxy = 1
		if h.Proxy.Kind == model.ProxyHTTP {
			s.proxy = 2
		}
		s.proxyAddr, s.proxyUser = h.Proxy.Addr, h.Proxy.User
	}
}

func hopsToIDs(h []model.Hop) []model.ID {
	out := make([]model.ID, len(h))
	for i, x := range h {
		out[i] = x.HostID
	}
	return out
}

// build turns the form into a host (without storing secrets).
func (a *App) buildHost() (model.Host, error) { //nolint:gocyclo // one check per form field, in the order of the form
	s := &a.sheet
	h := model.Host{
		ID: s.editing, Name: strings.TrimSpace(s.name), Addr: strings.TrimSpace(s.addr),
		User: strings.TrimSpace(s.user), OS: osFromIndex(s.os), Tags: slices.Clone(s.tags),
		StartDir: strings.TrimSpace(s.startDir), StartCmd: strings.TrimSpace(s.startCmd),
	}
	if h.ID == "" {
		h.ID = model.NewID()
	}
	if p := strings.TrimSpace(s.port); p != "" && p != "22" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return h, model.ErrPortInvalid
		}
		h.Port = n
	}
	if k := strings.TrimSpace(s.keepAlive); k != "" {
		n, err := strconv.Atoi(k)
		if err != nil || n < 0 {
			return h, model.ErrPortInvalid
		}
		h.KeepAlive = n
	}
	if s.policy == 1 {
		h.HostKeyPolicy = model.HostKeyStrict
	}
	primary := model.Auth{}
	switch s.auth {
	case authPassword:
		primary = model.Auth{Kind: model.AuthPassword}
	case authKey:
		primary = model.Auth{Kind: model.AuthKey, KeyPath: strings.TrimSpace(s.keyPath)}
	case authAgent:
		primary = model.Auth{Kind: model.AuthAgent}
	case authKI:
		primary = model.Auth{Kind: model.AuthKeyboardInteractive}
	}
	if primary.Kind == s.existingAuth.Kind {
		primary.SecretRef = s.existingAuth.SecretRef
	}
	h.Auth = []model.Auth{primary}
	for _, r := range s.rest {
		if r.Kind != primary.Kind {
			h.Auth = append(h.Auth, r)
		}
	}
	for _, id := range s.jump {
		h.Jump = append(h.Jump, model.Hop{HostID: id})
	}
	switch s.proxy {
	case 1, 2:
		kind := model.ProxySOCKS5
		if s.proxy == 2 {
			kind = model.ProxyHTTP
		}
		h.Proxy = &model.Proxy{Kind: kind, Addr: strings.TrimSpace(s.proxyAddr), User: strings.TrimSpace(s.proxyUser)}
		if s.editing != "" {
			if old, ok := a.d.Library.Get(s.editing); ok && old.Proxy != nil {
				h.Proxy.SecretRef = old.Proxy.SecretRef
			}
		}
	}
	if err := h.Validate(); err != nil {
		return h, fmt.Errorf("checking the form: %w", err)
	}
	return h, nil
}

// storeSecrets saves what the user typed and sets the references on h.
func (a *App) storeSecrets(ctx context.Context, h *model.Host, persist bool) error {
	s := &a.sheet
	put := func(ref, v string) error {
		if v == "" {
			return nil
		}
		if persist && s.remember {
			if err := a.d.Secrets.Set(ctx, ref, []byte(v)); err != nil && !isNotPersisted(err) {
				return fmt.Errorf("saving to the keychain: %w", err)
			}
			return nil
		}
		return a.d.Secrets.SetSession(ctx, ref, []byte(v)) //nolint:wrapcheck // memory store, cannot fail meaningfully
	}
	if len(h.Auth) > 0 && s.secret != "" {
		switch h.Auth[0].Kind {
		case model.AuthPassword, model.AuthKey:
			kind := "password"
			if h.Auth[0].Kind == model.AuthKey {
				kind = "key-passphrase"
			}
			if h.Auth[0].SecretRef == "" {
				h.Auth[0].SecretRef = model.SecretRef(h.ID, kind)
			}
			if err := put(h.Auth[0].SecretRef, s.secret); err != nil {
				return err
			}
		case model.AuthAgent, model.AuthKeyboardInteractive:
		}
	}
	if h.Proxy != nil && s.proxyPass != "" {
		if h.Proxy.SecretRef == "" {
			h.Proxy.SecretRef = model.SecretRef(h.ID, "proxy")
		}
		if err := put(h.Proxy.SecretRef, s.proxyPass); err != nil {
			return err
		}
	}
	return nil
}

func isNotPersisted(err error) bool { return errors.Is(err, secrets.ErrNotPersisted) }

// save stores the host and returns it.
func (a *App) saveSheet() (model.Host, bool) {
	s := &a.sheet
	h, err := a.buildHost()
	if err != nil {
		s.saveErr = a.formError(err)
		return h, false
	}
	if err := a.storeSecrets(context.Background(), &h, true); err != nil {
		s.saveErr = err.Error()
		return h, false
	}
	if s.editing == "" {
		var saved model.Host
		if saved, err = a.d.Library.Add(h); err == nil {
			h = saved
		}
	} else {
		err = a.d.Library.Update(h)
	}
	if err != nil {
		s.saveErr = a.formError(err)
		return h, false
	}
	return h, true
}

func (a *App) formError(err error) string {
	switch {
	case errors.Is(err, model.ErrNameEmpty):
		return a.T(kFormNameEmpty)
	case errors.Is(err, model.ErrAddrEmpty):
		return a.T(kFormAddrEmpty)
	case errors.Is(err, model.ErrUserEmpty):
		return a.T(kFormUserEmpty)
	case errors.Is(err, model.ErrPortInvalid):
		return a.T(kFormPortInvalid)
	case errors.Is(err, hostlib.ErrDuplicateName):
		return a.T(kFormDuplicate)
	case errors.Is(err, model.ErrJumpLoop):
		return a.T(kFormJumpLoop)
	default:
		return err.Error()
	}
}

// runTest tries the form's host without saving it.
func (a *App) runTest() {
	s := &a.sheet
	h, err := a.buildHost()
	if err != nil {
		s.testErr, s.testOK = a.formError(err), ""
		return
	}
	// The test must not touch the keychain or the saved host: use session-only
	// secrets under a throwaway reference.
	h.ID = model.NewID()
	if err := a.storeSecretsTest(&h); err != nil {
		s.testErr = err.Error()
		return
	}
	s.testing, s.testOK, s.testErr = true, "", ""
	s.testSeq++
	seq := s.testSeq
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res, err := a.d.Connector.Test(ctx, h)
		a.Post(func() {
			if seq != a.sheet.testSeq {
				return
			}
			a.sheet.testing = false
			if err != nil {
				a.sheet.testErr = a.explain(err)
				return
			}
			if res.OS != "" {
				a.sheet.os = hostMatchesOS(res.OS)
			}
			name := res.OSName
			if name == "" {
				name = string(res.OS)
			}
			a.sheet.testOK = a.T(kTestOK, res.Latency.Round(time.Millisecond).Milliseconds(), name)
		})
	}()
}

func (a *App) storeSecretsTest(h *model.Host) error {
	s := &a.sheet
	ctx := context.Background()
	if len(h.Auth) > 0 && s.secret != "" && (h.Auth[0].Kind == model.AuthPassword || h.Auth[0].Kind == model.AuthKey) {
		h.Auth[0].SecretRef = "test/" + string(h.ID) + "/secret"
		if err := a.d.Secrets.SetSession(ctx, h.Auth[0].SecretRef, []byte(s.secret)); err != nil {
			return fmt.Errorf("preparing the test: %w", err)
		}
	}
	if h.Proxy != nil && s.proxyPass != "" {
		h.Proxy.SecretRef = "test/" + string(h.ID) + "/proxy"
		if err := a.d.Secrets.SetSession(ctx, h.Proxy.SecretRef, []byte(s.proxyPass)); err != nil {
			return fmt.Errorf("preparing the test: %w", err)
		}
	}
	return nil
}

func (a *App) closeSheet() {
	s := &a.sheet
	// Secrets typed into the form live only as long as the form.
	s.secret, s.proxyPass = "", ""
	s.open = false
}

// build draws the sheet.
func (s *hostSheet) build(c *ui.Context, a *App) {
	if !s.open {
		return
	}
	open := true
	a.dialog(c, &open, func() { a.sheetBody(c) })
	if !open {
		a.closeSheet()
	}
}

func (a *App) sheetTitle() string {
	if a.sheet.editing != "" {
		return a.T(kEditHostTitle)
	}
	return a.T(kAddHostTitle)
}

func (a *App) sheetBody(c *ui.Context) {
	s := &a.sheet
	_, winH := c.Size()
	ui.Column(c).Width(620).MaxHeight(winH-48).Background(a.tk.Surface).Radius(18).
		Border(1, a.tk.BorderStrong).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.18)).Clip().Children(func() {
		// Header.
		ui.Row(c).Padding(16, 20).Gap(12).Shrink(0).Children(func() {
			a.osTile(c, osFromIndex(s.os), false, 36)
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, a.sheetTitle()).Bold().FontSize(17)
				a.caption(c, a.T(kSheetSub))
			})
			if s.editing == "" {
				imp := ui.Button(c, a.T(kImportSSHConfigShort)).FontSize(12).Height(28).Padding(0, 10)
				if imp.Clicked() {
					a.importSSHConfig()
				}
			}
		})
		ui.Divider(c)
		scroll := ui.Scroll(c).Grow(1).MinHeight(0)
		scroll.Children(func() {
			ui.Column(c).Padding(14, 20, 18).Gap(14).Children(func() {
				a.sheetBasics(c)
				a.sheetAuth(c)
				a.sheetRoute(c)
				a.sheetAdvanced(c)
			})
		})
		ui.Divider(c)
		a.sheetFooter(c)
	})
}

func (a *App) field(c *ui.Context, label string, body func()) {
	ui.Column(c).Gap(5).Grow(1).MinWidth(0).Children(func() {
		ui.Text(c, label).FontSize(12).Bold().TextColor(a.tk.Text2)
		body()
	})
}

func (a *App) section(c *ui.Context, title, hint string) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).Bold().FontSize(13)
		if hint != "" {
			a.caption(c, hint)
		}
	})
}

func (a *App) sheetBasics(c *ui.Context) {
	s := &a.sheet
	a.section(c, a.T(kSecBasics), "")
	ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
		a.field(c, a.T(kFName), func() {
			ui.TextInput(c, &s.name).Label(a.T(kFName)).AutoFocus().Placeholder("web-01")
		})
		a.field(c, a.T(kFTags), func() { a.tagEditor(c) })
	})
	ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
		a.field(c, a.T(kFAddr), func() { ui.TextInput(c, &s.addr).Label(a.T(kFAddr)).Placeholder("10.0.1.13") })
		ui.Column(c).Width(90).Gap(5).Children(func() {
			ui.Text(c, a.T(kFPort)).FontSize(12).Bold().TextColor(a.tk.Text2)
			ui.TextInput(c, &s.port).Label(a.T(kFPort))
		})
		a.field(c, a.T(kFUser), func() { ui.TextInput(c, &s.user).Label(a.T(kFUser)).Placeholder("ubuntu") })
	})
	a.field(c, a.T(kFOS), func() {
		ui.Segmented(c, &s.os, "Linux", "macOS", "Windows").Label(a.T(kFOS))
	})
}

func (a *App) tagEditor(c *ui.Context) {
	s := &a.sheet
	ui.Row(c).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
		for i, t := range s.tags {
			ui.Row(c).Key("tag:"+t).Height(24).Padding(0, 4, 0, 9).Gap(3).Radius(12).Background(a.tk.Surface3).Children(func() {
				dot(c, a.tagColor(t), 6)
				ui.Text(c, t).FontSize(11.5).Bold()
				if a.iconButton(c, iconClose, a.T(kRemoveTag, t), 16).Clicked() {
					s.tags = slices.Delete(s.tags, i, i+1)
				}
			})
		}
		in := ui.TextInput(c, &s.newTag).Placeholder(a.T(kAddTag)).Label(a.T(kAddTag)).Width(110)
		if in.Submitted() {
			if t := strings.TrimSpace(s.newTag); t != "" && !slices.Contains(s.tags, t) {
				s.tags = append(s.tags, t)
			}
			s.newTag = ""
		}
	})
}

func (a *App) sheetAuth(c *ui.Context) {
	s := &a.sheet
	a.section(c, a.T(kSecAuth), a.T(kSecAuthHint))
	ui.Segmented(c, &s.auth, a.T(kAuthPassword), a.T(kAuthKey), a.T(kAuthAgent), a.T(kAuthKI)).Label(a.T(kSecAuth))
	switch s.auth {
	case authKey:
		a.field(c, a.T(kFKeyFile), func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c, &s.keyPath).Label(a.T(kFKeyFile)).Placeholder("~/.ssh/id_ed25519").Grow(1)
				if a.d.PickFile != nil && ui.Button(c, a.T(kChoose)).Clicked() {
					go a.pickKey()
				}
			})
			a.caption(c, a.T(kKeyFormats))
		})
		a.secretField(c, a.T(kFPassphrase))
	case authPassword:
		a.secretField(c, a.T(kFPassword))
	case authAgent:
		a.caption(c, a.T(kAgentHint))
	case authKI:
		a.caption(c, a.T(kKIHint))
	}
}

func (a *App) pickKey() {
	path, err := a.d.PickFile(a.T(kFKeyFile))
	if err != nil || path == "" {
		return
	}
	a.Post(func() { a.sheet.keyPath = path })
}

func (a *App) secretField(c *ui.Context, label string) {
	s := &a.sheet
	a.field(c, label, func() {
		ui.Row(c).Gap(10).Children(func() {
			in := ui.TextInput(c, &s.secret).Label(label).Grow(1)
			if !s.showSecret {
				in.Password()
			}
			if a.iconButton(c, iconEye, a.T(kShow), 28).Clicked() {
				s.showSecret = !s.showSecret
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Switch(c, &s.remember).Label(a.T(kRememberKeychain))
				ui.Text(c, a.T(kRememberKeychain)).FontSize(12)
			})
		})
		if s.editing != "" && s.secret == "" {
			a.caption(c, a.T(kSecretKept))
		}
	})
}

func (a *App) sheetRoute(c *ui.Context) {
	s := &a.sheet
	a.section(c, a.T(kSecRoute), a.T(kSecRouteHint))
	ui.Row(c).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
		a.routeChip(c, a.T(kRouteLocal), a.T(kRouteStart), nil)
		for i, id := range s.jump {
			name := string(id)
			if h, ok := a.d.Library.Get(id); ok {
				name = h.Name
			}
			ui.Icon(c, iconChevronR).FontSize(12).TextColor(a.tk.Text3)
			a.routeChip(c, name, a.T(kRouteJump), func() { s.jump = slices.Delete(s.jump, i, i+1) })
		}
		ui.Icon(c, iconChevronR).FontSize(12).TextColor(a.tk.Text3)
		target := strings.TrimSpace(s.name)
		if target == "" {
			target = a.T(kRouteTarget)
		}
		a.routeChip(c, target, a.T(kRouteTarget), nil)
	})
	var opts []string
	for _, h := range a.d.Library.Hosts() {
		if h.ID != s.editing && !slices.Contains(s.jump, h.ID) {
			opts = append(opts, h.Name)
		}
	}
	if len(opts) > 0 {
		a.field(c, a.T(kAddJump), func() {
			sel := s.jumpPick
			pick := ui.Select(c, &sel, append([]string{""}, opts...)).Label(a.T(kAddJump)).Width(260)
			if pick.Changed() && sel != "" {
				for _, h := range a.d.Library.Hosts() {
					if h.Name == sel {
						s.jump = append(s.jump, h.ID)
						break
					}
				}
			}
		})
	}
	a.field(c, a.T(kFProxy), func() {
		ui.Segmented(c, &s.proxy, a.T(kProxyNone), "SOCKS5", "HTTP").Label(a.T(kFProxy))
		if s.proxy != 0 {
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c, &s.proxyAddr).Label(a.T(kProxyAddr)).Placeholder("127.0.0.1:1080").Grow(2)
				ui.TextInput(c, &s.proxyUser).Label(a.T(kProxyUser)).Placeholder(a.T(kProxyUser)).Grow(1)
				ui.TextInput(c, &s.proxyPass).Label(a.T(kProxyPass)).Placeholder(a.T(kProxyPass)).Password().Grow(1)
			})
		}
	})
}

func (a *App) routeChip(c *ui.Context, title, sub string, remove func()) {
	ui.Row(c).Key("route:"+title+sub).Height(40).Padding(0, 10).Gap(8).Radius(10).Background(a.tk.Surface3).Children(func() {
		ui.Column(c).Children(func() {
			ui.Text(c, title).Bold().FontSize(12.5).NoWrap()
			a.caption(c, sub)
		})
		if remove != nil && a.iconButton(c, iconClose, a.T(kRemoveHop, title), 18).Clicked() {
			remove()
		}
	})
}

func (a *App) sheetAdvanced(c *ui.Context) {
	s := &a.sheet
	a.section(c, a.T(kSecAdvanced), "")
	ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
		a.field(c, a.T(kFKeepAlive), func() { ui.TextInput(c, &s.keepAlive).Label(a.T(kFKeepAlive)).Placeholder("30") })
		a.field(c, a.T(kFStartDir), func() { ui.TextInput(c, &s.startDir).Label(a.T(kFStartDir)).Placeholder("/srv/app") })
		a.field(c, a.T(kFHostKey), func() {
			ui.Segmented(c, &s.policy, a.T(kPolicyAsk), a.T(kPolicyStrict)).Label(a.T(kFHostKey))
		})
	})
	a.field(c, a.T(kFStartCmd), func() { ui.TextInput(c, &s.startCmd).Label(a.T(kFStartCmd)).Placeholder("tmux attach || tmux") })
}

func (a *App) sheetFooter(c *ui.Context) {
	s := &a.sheet
	ui.Row(c).Padding(12, 20).Gap(10).Shrink(0).Background(a.tk.Surface2).Children(func() {
		ui.Row(c).Gap(8).Grow(1).MinWidth(0).Children(func() {
			switch {
			case s.testing:
				ui.Icon(c, iconRefresh).FontSize(15).TextColor(a.tk.Text2)
				ui.Text(c, a.T(kTesting)).FontSize(12).TextColor(a.tk.Text2)
			case s.saveErr != "":
				ui.Icon(c, iconAlert).FontSize(15).TextColor(a.tk.Red)
				ui.Text(c, s.saveErr).FontSize(12).TextColor(a.tk.Red).Grow(1).MinWidth(0)
			case s.testErr != "":
				ui.Icon(c, iconAlert).FontSize(15).TextColor(a.tk.Red)
				ui.Text(c, s.testErr).FontSize(12).TextColor(a.tk.Red).Grow(1).MinWidth(0)
			case s.testOK != "":
				ui.Icon(c, iconCheck).FontSize(15).TextColor(a.tk.Green)
				ui.Text(c, s.testOK).FontSize(12).Bold().TextColor(a.tk.Green)
			case s.importMsg != "":
				ui.Text(c, s.importMsg).FontSize(12).TextColor(a.tk.Text2)
			}
		})
		if ui.Button(c, a.T(kCancel)).Clicked() {
			a.closeSheet()
		}
		if ui.Button(c, a.T(kTestConnection)).Disabled(s.testing).Clicked() {
			s.saveErr = ""
			a.runTest()
		}
		save := ui.PrimaryButton(c, a.T(kSaveConnect))
		if save.Clicked() {
			s.saveErr = ""
			if h, ok := a.saveSheet(); ok {
				a.closeSheet()
				a.openHost(h, false)
			}
		}
	})
}

// importSSHConfig adds the hosts of ~/.ssh/config to the library.
func (a *App) importSSHConfig() {
	hosts, warns, err := a.d.ImportSSHConfig()
	msg := ""
	switch {
	case err != nil:
		msg = a.T(kImportFailed, err)
	case len(hosts) == 0:
		msg = a.T(kImportNone)
	default:
		added, skipped, aerr := a.d.Library.AddMany(hosts)
		if aerr != nil {
			msg = a.T(kImportFailed, aerr)
		} else {
			msg = a.T(kImportDone, len(added), skipped)
			if len(warns) > 0 {
				msg += " " + a.T(kImportWarnings, len(warns))
			}
		}
	}
	a.sheet.importMsg = msg
	a.toast(msg)
	a.sshConfigHosts = nil
	a.sshConfigLoaded = false
}
