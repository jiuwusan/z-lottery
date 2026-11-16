package assets_test

import (
	"testing"

	"github.com/dop251/goja"
	assets "github.com/jiuwusan/z-lottery/lottery"
)

const installBrowser = `
const listeners = {}, mediaListeners = {};
const nodes = {};
function node() {
  return { hidden: true, disabled: false, textContent: '', open: false, children: [],
    addEventListener(type, fn) { this[type === 'close' ? 'closeEvent' : type] = fn; },
    replaceChildren(...items) { this.children = items; },
    append(...items) { this.children.push(...items); },
    showModal() { this.open = true; }, close() { this.open = false; this.closeEvent?.(); },
    focus() { this.focused = true; }
  };
}
for (const id of ['install-button', 'install-dialog', 'install-description', 'install-steps', 'close-install']) nodes[id] = node();
globalThis.window = globalThis;
globalThis.document = { getElementById(id) { return nodes[id]; }, createElement: node };
globalThis.navigator = { userAgent: 'Windows Chrome', platform: 'Win32', maxTouchPoints: 0, standalone: false };
globalThis.location = { protocol: 'https:' };
globalThis.isSecureContext = true;
globalThis.addEventListener = (type, fn) => { listeners[type] = fn; };
globalThis.display = { matches: false, addEventListener(type, fn) { mediaListeners[type] = fn; } };
globalThis.matchMedia = () => display;
function check(ok, message) { if (!ok) throw Error(message); }
function guidance() { return nodes['install-description'].textContent + nodes['install-steps'].children.map(n => n.textContent).join(' '); }
function promptEvent(choice = 'dismissed') {
  return { prevented: false, calls: 0,
    preventDefault() { this.prevented = true; },
    prompt() { this.calls++; return Promise.resolve(); },
    userChoice: Promise.resolve({ outcome: choice })
  };
}
`

func TestInstallInteraction(t *testing.T) {
	cases := []struct {
		name, setup, assertion string
	}{
		{"desktop_guide", "", `nodes['install-button'].click(); check(guidance().includes('Chrome') && guidance().includes('Edge'), guidance()); check(nodes['install-dialog'].open, 'guide should open'); nodes['close-install'].click(); check(!nodes['install-dialog'].open, 'guide should close');`},
		{"iphone", `navigator.userAgent = 'iPhone Safari';`, `nodes['install-button'].click(); check(guidance().includes('分享') && guidance().includes('主屏幕'), guidance());`},
		{"ipad_desktop_agent", `navigator.userAgent = 'Macintosh Safari'; navigator.platform = 'MacIntel'; navigator.maxTouchPoints = 5;`, `nodes['install-button'].click(); check(guidance().includes('主屏幕'), guidance());`},
		{"android", `navigator.userAgent = 'Android Chrome';`, `nodes['install-button'].click(); check(guidance().includes('主屏幕'), guidance());`},
		{"mac_safari", `navigator.userAgent = 'Macintosh Version/18.0 Safari'; navigator.platform = 'MacIntel';`, `nodes['install-button'].click(); check(guidance().includes('程序坞'), guidance());`},
		{"embedded_browser", `navigator.userAgent = 'iPhone MicroMessenger';`, `nodes['install-button'].click(); check(guidance().includes('浏览器') && guidance().includes('Safari'), guidance());`},
		{"http", `isSecureContext = false; location.protocol = 'http:';`, `nodes['install-button'].click(); check(guidance().includes('HTTPS'), guidance());`},
		{"file", `location.protocol = 'file:';`, `nodes['install-button'].click(); check(guidance().includes('HTTPS'), guidance());`},
		{"standalone", `display.matches = true;`, `check(nodes['install-button'].hidden, 'standalone should hide install');`},
		{"ios_standalone", `navigator.standalone = true;`, `check(nodes['install-button'].hidden, 'iOS standalone should hide install');`},
		{"native_dismissed", "", `const e = promptEvent(); listeners.beforeinstallprompt(e); check(e.prevented, 'defer automatic prompt'); nodes['install-button'].click(); check(e.calls === 1, 'native prompt should open');`},
		{"prompt_consumed_once", "", `const e = promptEvent(); listeners.beforeinstallprompt(e); nodes['install-button'].click(); nodes['install-button'].click(); check(e.calls === 1, 'no duplicate prompt');`},
		{"accepted_not_installed", "", `const e = promptEvent('accepted'); listeners.beforeinstallprompt(e); nodes['install-button'].click(); check(!nodes['install-button'].hidden, 'accept is not installation completion');`},
		{"install_complete", "", `listeners.appinstalled(); check(nodes['install-button'].hidden, 'installed should hide button'); const e = promptEvent(); listeners.beforeinstallprompt(e); check(nodes['install-button'].hidden, 'late prompt must not revive installed button');`},
		{"display_change", "", `display.matches = true; mediaListeners.change(); check(nodes['install-button'].hidden, 'standalone change should hide button'); display.matches = false; mediaListeners.change(); check(!nodes['install-button'].hidden, 'browser mode should restore guide');`},
		{"prompt_failed", "", `const e = promptEvent(); e.prompt = () => Promise.reject(Error('unavailable')); listeners.beforeinstallprompt(e); nodes['install-button'].click();`},
		{"installed_during_prompt", "", `const e = promptEvent(); e.userChoice = new Promise(resolve => { globalThis.finishPrompt = resolve; }); listeners.beforeinstallprompt(e); nodes['install-button'].click(); listeners.appinstalled(); finishPrompt({outcome:'dismissed'});`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := goja.New()
			for _, source := range []string{installBrowser, tc.setup} {
				if _, err := vm.RunString(source); err != nil {
					t.Fatal(err)
				}
			}
			source, err := assets.Files.ReadFile("install.js")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := vm.RunScript("install.js", string(source)); err != nil {
				t.Fatal(err)
			}
			if _, err := vm.RunString(tc.assertion); err != nil {
				t.Fatal(err)
			}
			post := ""
			switch tc.name {
			case "native_dismissed", "prompt_failed", "prompt_consumed_once":
				post = `check(!nodes['install-button'].disabled && !nodes['install-button'].hidden, 'cancel/error should allow retry'); check(nodes['install-dialog'].open, 'cancel/error should show guide'); nodes['install-button'].click();`
			case "accepted_not_installed":
				post = `check(!nodes['install-button'].hidden, 'accepted must not pretend installed');`
			case "installed_during_prompt":
				post = `check(nodes['install-button'].hidden && !nodes['install-dialog'].open, 'late choice cannot revive guide');`
			}
			if _, err := vm.RunString(post); err != nil {
				t.Fatal(err)
			}
		})
	}
}
