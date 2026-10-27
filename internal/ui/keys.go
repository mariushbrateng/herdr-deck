package ui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Down, Up       key.Binding
	Fold           key.Binding
	Linear, Figma  key.Binding
	Notion, PR     key.Binding
	Localhost      key.Binding
	Pane, Editor   key.Binding
	DevUp, DevStop key.Binding
	Diff           key.Binding
	Report, Drawer key.Binding
	Priority, Page key.Binding
	Move, View     key.Binding
	Grow, Shrink   key.Binding
	Sources, Help  key.Binding
	Settings       key.Binding
	News           key.Binding
	Projects       key.Binding
	ImpactPane     key.Binding
	Check          key.Binding
	NextTab        key.Binding
	PrevTab        key.Binding
	NextView       key.Binding
	PrevView       key.Binding
	Focus, Unfocus key.Binding
	PageDown       key.Binding
	PageUp         key.Binding
	Back, Quit     key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Down:      key.NewBinding(key.WithKeys("j", "down")),
		Up:        key.NewBinding(key.WithKeys("k", "up")),
		Fold:      key.NewBinding(key.WithKeys("space")),
		Linear:    key.NewBinding(key.WithKeys("l")),
		Figma:     key.NewBinding(key.WithKeys("f")),
		Notion:    key.NewBinding(key.WithKeys("n")),
		PR:        key.NewBinding(key.WithKeys("g")),
		Localhost: key.NewBinding(key.WithKeys("o")),
		Pane:      key.NewBinding(key.WithKeys("enter")),
		Editor:    key.NewBinding(key.WithKeys("e")),
		DevUp:     key.NewBinding(key.WithKeys("u")),
		DevStop:   key.NewBinding(key.WithKeys("U")),
		Diff:      key.NewBinding(key.WithKeys("d")),
		Report:    key.NewBinding(key.WithKeys("r")),
		Priority:  key.NewBinding(key.WithKeys("P")),
		Move:      key.NewBinding(key.WithKeys("m")),
		View:      key.NewBinding(key.WithKeys("V")),
		Page:      key.NewBinding(key.WithKeys("R")),
		Drawer:    key.NewBinding(key.WithKeys("z")),
		Grow:      key.NewBinding(key.WithKeys("+", "=")),
		Shrink:    key.NewBinding(key.WithKeys("-")),
		Sources:   key.NewBinding(key.WithKeys("!")),
		Help:      key.NewBinding(key.WithKeys("?")),
		Settings:  key.NewBinding(key.WithKeys("s")),
		News:      key.NewBinding(key.WithKeys("w")),
		Projects:  key.NewBinding(key.WithKeys("p")),
		// O opens the Impact view in a pane of its own; o is localhost.
		ImpactPane: key.NewBinding(key.WithKeys("O")),
		Check:      key.NewBinding(key.WithKeys("c")),
		NextTab:    key.NewBinding(key.WithKeys("]")),
		PrevTab:    key.NewBinding(key.WithKeys("[")),
		NextView:   key.NewBinding(key.WithKeys("right")),
		PrevView:   key.NewBinding(key.WithKeys("left")),
		Focus:      key.NewBinding(key.WithKeys("tab")),
		Unfocus:    key.NewBinding(key.WithKeys("shift+tab")),
		PageDown:   key.NewBinding(key.WithKeys("pgdown", "ctrl+d")),
		PageUp:     key.NewBinding(key.WithKeys("pgup", "ctrl+u")),
		Back:       key.NewBinding(key.WithKeys("esc")),
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c")),
	}
}
