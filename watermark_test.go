package main

import (
	"image/color"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestWatermarkOpacityAndColour(t *testing.T) {
	got := color.RGBAModel.Convert(blendWatermark(color.White)).(color.RGBA)
	if got != (color.RGBA{46, 44, 53, 255}) {
		t.Fatalf("expected 10%% white over panel background, got %+v", got)
	}
	if artworkColour(40, 25) == artworkColour(40, 35) {
		t.Fatal("skin and shirt have the same colour")
	}
}

func TestWatermarkPreservesPanelText(t *testing.T) {
	content := accent.Render("LIVE OUTPUT") + "\n\n" + dim.Render("✓ 你好  e\u0301  👩‍💻 preserve spacing") + "\n\n" + "short line"
	before := panel.Width(76).Height(30).Render(content)
	after := mainPanel(content, 76, 30)
	w, h := lipgloss.Size(before)
	if aw, ah := lipgloss.Size(after); aw != w || ah != h {
		t.Fatalf("panel changed size: %dx%d → %dx%d", w, h, aw, ah)
	}
	original := lipgloss.NewCanvas(w, h).Compose(lipgloss.NewLayer(before))
	result := lipgloss.NewCanvas(w, h).Compose(lipgloss.NewLayer(after))
	marks := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			cell, got := original.CellAt(x, y), result.CellAt(x, y)
			if cell != nil && cell.Content != " " {
				if !cell.Equal(got) {
					t.Fatalf("foreground changed at %d,%d", x, y)
				}
			} else if got != nil && got.Content != " " && got.Content != "" {
				marks++
			}
		}
	}
	if marks < 20 {
		t.Fatalf("watermark missing: only %d artwork cells", marks)
	}
}

func TestWatermarkCentred(t *testing.T) {
	art := watermark(100, 30)
	left, right := 100, -1
	for y := 0; y < 30; y++ {
		for x := 0; x < 100; x++ {
			if c := art.CellAt(x, y); c != nil && c.Content != " " && c.Content != "" {
				left = min(left, x)
				right = max(right, x)
			}
		}
	}
	if left < 30 || right > 70 || right < left {
		t.Fatalf("art is not centred: x=%d..%d", left, right)
	}
}

func TestWorkshopEnterAndShiftEnter(t *testing.T) {
	m := newModel(Config{Model: "test/model"}, true)
	m.openWorkshop(m.repos[0])
	m.input.SetValue("first line")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	m = updated.(model)
	if m.input.Value() != "first line\n" || len(m.messages) != 0 {
		t.Fatalf("shift+enter sent or lost the newline: %q", m.input.Value())
	}
	m.input.InsertString("second line")
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if cmd == nil || !m.busy || len(m.messages) != 1 || m.messages[0].Content != "first line\nsecond line" || m.input.Value() != "" {
		t.Fatal("enter did not send the multiline message")
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.busy = false
	m.page = review
	m.input.SetValue("prompt")
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if m.input.Value() != "prompt\n" || m.busy {
		t.Fatal("enter stopped being a newline in the prompt editor")
	}
}
