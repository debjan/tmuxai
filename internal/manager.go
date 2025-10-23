package internal

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alvinunreal/tmuxai/config"
	"github.com/alvinunreal/tmuxai/logger"
	"github.com/alvinunreal/tmuxai/system"
	"github.com/fatih/color"
)

type AIResponse struct {
	Message                string
	SendKeys               []string
	ExecCommand            []string
	PasteMultilineContent  string
	RequestAccomplished    bool
	ExecPaneSeemsBusy      bool
	WaitingForUserResponse bool
	NoComment              bool
}

// Parsed only when pane is prepared
type CommandExecHistory struct {
	Command string
	Output  string
	Code    int
}

// Manager represents the TmuxAI manager agent
type Manager struct {
	Config           *config.Config
	AiClient         *AiClient
	Status           string // running, waiting, done
	PaneId           string
	ExecPane         *system.TmuxPaneDetails
	Messages         []ChatMessage
	ExecHistory      []CommandExecHistory
	WatchMode        bool
	OS               string
	SessionOverrides map[string]interface{} // session-only config overrides
	LoadedKBs        map[string]string      // Loaded knowledge bases (name -> content)

	// Functions for mocking
	confirmedToExec  func(command string, prompt string, edit bool) (bool, string)
	getTmuxPanesInXml func(config *config.Config) string
}

// NewManager creates a new manager agent
func NewManager(cfg *config.Config) (*Manager, error) {

	paneId, err := system.TmuxCurrentPaneId()
	if err != nil {
		// If we're not in a tmux session, start a new session and execute the same command
		paneId, err = system.TmuxCreateSession()
		if err != nil {
			return nil, fmt.Errorf("system.TmuxCreateSession failed: %w", err)
		}
		args := strings.Join(os.Args[1:], " ")

		_ = system.TmuxSendCommandToPane(paneId, "tmuxai "+args, true)
		// shell initialization may take some time
		time.Sleep(1 * time.Second)
		_ = system.TmuxSendCommandToPane(paneId, "Enter", false)
		err = system.TmuxAttachSession(paneId)
		if err != nil {
			return nil, fmt.Errorf("system.TmuxAttachSession failed: %w", err)
		}
		os.Exit(0)
	}

	aiClient := NewAiClient(cfg)
	os := system.GetOSDetails()

	manager := &Manager{
		Config:           cfg,
		AiClient:         aiClient,
		PaneId:           paneId,
		Messages:         []ChatMessage{},
		ExecPane:         &system.TmuxPaneDetails{},
		OS:               os,
		SessionOverrides: make(map[string]interface{}),
		LoadedKBs:        make(map[string]string),
	}

	// Set the config manager in the AI client
	aiClient.SetConfigManager(manager)

	manager.confirmedToExec = manager.confirmedToExecFn
	manager.getTmuxPanesInXml = manager.getTmuxPanesInXmlFn

	manager.InitExecPane()

	// Auto-load knowledge bases from config
	manager.autoLoadKBs()

	return manager, nil
}

// Start starts the manager agent
func (m *Manager) Start(initMessage string) error {
	cliInterface := NewCLIInterface(m)
	if initMessage != "" {
		logger.Info("Initial task provided: %s", initMessage)
	}
	if err := cliInterface.Start(initMessage); err != nil {
		logger.Error("Failed to start CLI interface: %v", err)
		return err
	}

	return nil
}

func (m *Manager) Println(msg string) {
	fmt.Println(m.GetPrompt() + msg)
}

func (m *Manager) GetConfig() *config.Config {
	return m.Config
}

// getPrompt returns the prompt string with color
func (m *Manager) GetPrompt() string {
	tmuxaiColor := color.New(color.BgGreen, color.FgBlack)
	stateColor := color.New(color.BgHiGreen, color.FgBlack)
	arrowColor := color.New(color.BgBlack, color.FgHiGreen)
	// sepColor := color.New(color.BgGreen, color.FgHiGreen)
	modelColor := color.New(color.BgGreen, color.FgBlack)

	var stateSymbol string
	switch m.Status {
	case "running":
		stateSymbol = ""
	case "waiting":
		stateSymbol = ""
	case "done":
		stateSymbol = ""
	default:
		stateSymbol = ""
	}
	if m.WatchMode {
		stateSymbol = ""
	}

	prompt := tmuxaiColor.Sprint("")
	// prompt += sepColor.Sprint("")
	// Show current model if it's not the default or first available model

	currentModel := m.GetModelsDefault()
	availableModels := m.GetAvailableModels()
	if len(availableModels) > 0 {
		// Get the "expected" model (configured default or first available)
		expectedModel := m.Config.DefaultModel
		if expectedModel == "" && len(availableModels) > 0 {
			expectedModel = availableModels[0] // First model as default
		}

		// Show model if current is different from expected
		if currentModel != "" && currentModel != expectedModel {
			prompt += " " + modelColor.Sprint("["+currentModel+"]")
		}
	}

	if stateSymbol != "" {
		prompt += stateColor.Sprint(" "+stateSymbol+" ")
	}
	prompt += arrowColor.Sprint("") + " "
	return prompt
}

func (ai *AIResponse) String() string {
	return fmt.Sprintf(`
	Message: %s
	SendKeys: %v
	ExecCommand: %v
	PasteMultilineContent: %s
	RequestAccomplished: %v
	ExecPaneSeemsBusy: %v
	WaitingForUserResponse: %v
	NoComment: %v
`,
		ai.Message,
		ai.SendKeys,
		ai.ExecCommand,
		ai.PasteMultilineContent,
		ai.RequestAccomplished,
		ai.ExecPaneSeemsBusy,
		ai.WaitingForUserResponse,
		ai.NoComment,
	)
}
