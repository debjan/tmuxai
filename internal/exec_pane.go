package internal

import (
	"bufio"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alvinunreal/tmuxai/logger"
	"github.com/alvinunreal/tmuxai/system"
)

// GetAvailablePane finds an available pane or creates a new one if none are available
func (m *Manager) GetAvailablePane() system.TmuxPaneDetails {
	panes, _ := m.GetTmuxPanes()
	for _, pane := range panes {
		if !pane.IsTmuxAiPane {
			logger.Info("Found available pane: %s", pane.Id)
			return pane
		}
	}

	return system.TmuxPaneDetails{}
}

func (m *Manager) InitExecPane() {
	availablePane := m.GetAvailablePane()
	if availablePane.Id == "" {
		_, _ = system.TmuxCreateNewPane(m.PaneId)
		availablePane = m.GetAvailablePane()
	}
	m.ExecPane = &availablePane
}

func (m *Manager) PrepareExecPaneWithShell(shell string) {
	m.ExecPane.Refresh(m.GetMaxCaptureLines())
	if m.ExecPane.IsPrepared && m.ExecPane.Shell != "" {
		return
	}

	var ps1Command string
	switch shell {
	case "zsh":
		// Format: user@host:dir[status] with proper  character before command
		ps1Command = `export PROMPT=$'%{\033[30m\033[1;102m%} %n@%m:%~[$?]%{\033[0m%} '` + `; export RPROMPT=""`
	case "bash":
		// Format: user@host:dir[status] with proper  character before command
		ps1Command = `export PS1='\[\033[30m\033[1;102m\] \u@\h:\w[$?]\[\033[40m\]\[\033[1;32m] '`
	case "fish":
		// Format: user@host:dir[status] with proper  character before command
		ps1Command = `function fish_prompt; set_color -b green black; printf ' %s@%s:%s[%s]\033[0m' $USER (hostname -s) (prompt_pwd) (status); printf ' '; set_color normal; end`
	default:
		errMsg := fmt.Sprintf("Shell '%s' in pane %s is recognized but not yet supported for PS1 modification.", shell, m.ExecPane.Id)
		logger.Info(errMsg)
		return
	}

	_ = system.TmuxSendCommandToPane(m.ExecPane.Id, ps1Command, true)
	_ = system.TmuxSendCommandToPane(m.ExecPane.Id, "C-l", false)
}

func (m *Manager) PrepareExecPane() {
	m.PrepareExecPaneWithShell(m.ExecPane.CurrentCommand)
}

func (m *Manager) ExecWaitCapture(command string) (CommandExecHistory, error) {
	_ = system.TmuxSendCommandToPane(m.ExecPane.Id, command, true)

	// wait for keys to be sent, duo to sometimes ssh latency
	time.Sleep(500 * time.Millisecond)

	m.ExecPane.Refresh(m.GetMaxCaptureLines())

	animChars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	animIndex := 0
	for !strings.HasSuffix(m.ExecPane.LastLine, "]") && m.Status != "" {
		fmt.Printf("\r%s%s ", m.GetPrompt(), animChars[animIndex])
		animIndex = (animIndex + 1) % len(animChars)
		time.Sleep(500 * time.Millisecond)
		m.ExecPane.Refresh(m.GetMaxCaptureLines())
	}
	fmt.Print("\r\033[K")

	m.parseExecPaneCommandHistory()
	if len(m.ExecHistory) == 0 {
		logger.Error("Failed to parse command history from exec pane")
		return CommandExecHistory{}, fmt.Errorf("failed to parse command history from exec pane")
	}
	cmd := m.ExecHistory[len(m.ExecHistory)-1]
	logger.Debug("Command: %s\nOutput: %s\nCode: %d\n", cmd.Command, cmd.Output, cmd.Code)
	return cmd, nil
}

func (m *Manager) parseExecPaneCommandHistory() {
	m.parseExecPaneCommandHistoryWithContent("")
}

func (m *Manager) parseExecPaneCommandHistoryWithContent(testContent string) {
	if testContent == "" {
		m.ExecPane.Refresh(m.GetMaxCaptureLines())
	} else {
		m.ExecPane.Content = testContent
	}

	var history []CommandExecHistory

	var currentCommand *CommandExecHistory
	var outputBuilder strings.Builder

	// Regex: Capture status code (group 1), optionally capture command (group 2)
	// Making the command part optional handles prompts that only show status (like the last line).
	// ` ?` allows zero or one space after 
	promptRegex := regexp.MustCompile(`.*\[(\d+)\] ?(.*)$`)

	scanner := bufio.NewScanner(strings.NewReader(m.ExecPane.Content))

	for scanner.Scan() {
		line := scanner.Text()
		match := promptRegex.FindStringSubmatch(line)

		if len(match) >= 2 { // We need at least the status code match[1]
			// --- Found a prompt line ---
			// This prompt line *terminates* the previous command block
			// and provides its status code. It might also start a new command block.

			statusCodeStr := match[1]
			commandStr := "" // Default if only status code found (like the last line)
			if len(match) > 2 {
				commandStr = strings.TrimSpace(match[2]) // Command for the *next* block
			}

			// 1. Finalize the PREVIOUS command block (if one was active)
			if currentCommand != nil {
				// Parse the status code found on *this* line - it belongs to the *previous* command
				statusCode, err := strconv.Atoi(statusCodeStr)
				if err != nil {
					// This shouldn't happen with \d+ regex but check anyway
					fmt.Printf("Warning: Could not parse status code '%s' for previous command on line: %s\n", statusCodeStr, line)
					currentCommand.Code = -1 // Indicate parsing error
				} else {
					currentCommand.Code = statusCode // Assign correct status
				}

				// Assign collected output
				currentCommand.Output = strings.TrimSuffix(outputBuilder.String(), "\n")

				// Add the completed previous command block to results
				history = append(history, *currentCommand)

				// Reset for the next block
				outputBuilder.Reset()
				currentCommand = nil // Mark as no active command temporarily
			}

			// 2. If this prompt line ALSO contains a command, start the NEW block
			if commandStr != "" {
				currentCommand = &CommandExecHistory{
					Command: commandStr,
					Code:    -1, // Default/Unknown: Status code is determined by the *next* prompt
					// Output will be collected in outputBuilder starting from the next line
				}
			}

		} else {
			// --- Not a prompt line - Must be output ---
			if currentCommand != nil {
				// Append this line as output to the currently active command
				outputBuilder.WriteString(line)
				outputBuilder.WriteString("\n") // Preserve line breaks
			}
			// Ignore lines before the first *actual* command starts
			// (i.e., before the first prompt line that contains a command string)
		}
	}

	// --- After the loop ---
	// Handle the case where the input ends with output lines for the last command,
	// but without a final terminating prompt line.
	if currentCommand != nil {
		currentCommand.Output = strings.TrimSuffix(outputBuilder.String(), "\n")
		// Status code remains the default (-1) because the log ended before the next prompt
		// could provide the exit status.
		history = append(history, *currentCommand)
	}

	if err := scanner.Err(); err != nil {
		logger.Error("error reading input: %v", err)
	}

	// Update the manager's command history
	m.ExecHistory = history
}