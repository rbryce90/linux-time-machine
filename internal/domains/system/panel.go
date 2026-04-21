package system

import (
	"fmt"

	"github.com/rbryce90/linux-time-machine/internal/tui"
)

type panel struct {
	store Store
}

func (p *panel) Title() string { return "System" }
func (p *panel) View() string  { return fmt.Sprintf("[%s] placeholder", p.Title()) }

func (d *Domain) Panel() tui.Panel {
	return &panel{store: d.store}
}
