package ui

import (
	"fmt"
	"os/exec"

	tea "charm.land/bubbletea/v2"
)

type clipboardCopiedMsg string

// copyCmd sends text through OSC 52 and the first system clipboard command
// found on PATH, and flashes a report in the footer.
func copyCmd(text string) tea.Cmd {
	return tea.Batch(
		tea.SetClipboard(text),
		func() tea.Msg {
			tool := ""
			for _, cand := range []string{"wl-copy", "pbcopy", "xclip"} {
				if _, err := exec.LookPath(cand); err == nil {
					tool = cand
					break
				}
			}
			if tool != "" {
				var cmd *exec.Cmd
				if tool == "xclip" {
					cmd = exec.Command("xclip", "-selection", "clipboard")
				} else {
					cmd = exec.Command(tool)
				}
				stdin, err := cmd.StdinPipe()
				if err == nil {
					if err := cmd.Start(); err == nil {
						stdin.Write([]byte(text))
						stdin.Close()
						cmd.Wait()
					}
				}
			}
			how := "OSC 52"
			if tool != "" {
				how += " + " + tool
			}
			return clipboardCopiedMsg(fmt.Sprintf("Copied %d chars (%s)", len([]rune(text)), how))
		},
	)
}
