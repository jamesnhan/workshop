package tmux

import "errors"

// ErrHeadless is returned by NoBridge for all tmux operations.
var ErrHeadless = errors.New("tmux not available in headless mode")

// NoBridge implements Bridge with every method returning ErrHeadless.
// Used in headless (K8s) deployments where tmux is not installed.
type NoBridge struct{}

// NewNoBridge creates a NoBridge instance.
func NewNoBridge() *NoBridge { return &NoBridge{} }

func (n *NoBridge) ListSessions() ([]Session, error)                    { return nil, ErrHeadless }
func (n *NoBridge) CreateSession(name, startDir string) error           { return ErrHeadless }
func (n *NoBridge) KillSession(name string) error                       { return ErrHeadless }
func (n *NoBridge) RenameSession(oldName, newName string) error         { return ErrHeadless }
func (n *NoBridge) RenameWindow(target, newName string) error           { return ErrHeadless }
func (n *NoBridge) CreateWindow(session, name string) error             { return ErrHeadless }
func (n *NoBridge) SplitWindow(target string, horizontal bool) (string, error) {
	return "", ErrHeadless
}
func (n *NoBridge) SendKeys(target, keys string) error            { return ErrHeadless }
func (n *NoBridge) SendKeysLiteral(target, keys string) error     { return ErrHeadless }
func (n *NoBridge) SendKeysHex(target, hexStr string) error       { return ErrHeadless }
func (n *NoBridge) SendInput(target, data string) error           { return ErrHeadless }
func (n *NoBridge) CapturePane(target string, lines int) (string, error) {
	return "", ErrHeadless
}
func (n *NoBridge) CapturePanePlain(target string, lines int) (string, error) {
	return "", ErrHeadless
}
func (n *NoBridge) CapturePaneVisible(target string) (string, error) { return "", ErrHeadless }
func (n *NoBridge) CapturePaneAll(target string) (string, error)     { return "", ErrHeadless }
func (n *NoBridge) RunRaw(args ...string) (string, error)            { return "", ErrHeadless }
func (n *NoBridge) ResizePane(target string, cols, rows int) error   { return ErrHeadless }
func (n *NoBridge) PaneTTY(target string) (string, error)            { return "", ErrHeadless }
func (n *NoBridge) ListPanes(session string) ([]Pane, error)         { return nil, ErrHeadless }
func (n *NoBridge) LaunchAgent(cfg AgentConfig) (*AgentResult, error) {
	return nil, ErrHeadless
}
