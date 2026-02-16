package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func RunTUI(initial AppState) error {
	app := tview.NewApplication()

	tview.Styles.PrimitiveBackgroundColor = tcell.ColorBlack
	tview.Styles.ContrastBackgroundColor = tcell.ColorBlack
	tview.Styles.MoreContrastBackgroundColor = tcell.ColorBlack
	tview.Styles.PrimaryTextColor = tcell.ColorWhite
	tview.Styles.BorderColor = tcell.ColorGray
	tview.Styles.SecondaryTextColor = tcell.ColorWhite

	state := initial

	// --- widgets ---
	header := tview.NewTextView()
	header.SetDynamicColors(true)
	header.SetTextAlign(tview.AlignCenter)
	header.SetBackgroundColor(tcell.ColorBlack)

	results := tview.NewTextView()
	results.SetDynamicColors(true)
	results.SetScrollable(true)
	results.SetWrap(false)
	results.SetBorder(true)
	results.SetTitle("Report")
	results.SetBackgroundColor(tcell.ColorBlack)

	status := tview.NewTextView()
	status.SetDynamicColors(true)
	status.SetBorder(true)
	status.SetBackgroundColor(tcell.ColorBlack)

	help := tview.NewTextView()
	help.SetDynamicColors(true)
	help.SetText("[yellow]Enter[white]: apply field | [yellow]F/L/S/Q[white]: fetch/load/save/quit")
	help.SetBackgroundColor(tcell.ColorBlack)

	inShale := tview.NewInputField().SetLabel("Shale avg: ")
	inShard := tview.NewInputField().SetLabel("Shard avg: ")
	inA1 := tview.NewInputField().SetLabel("Helmet avg: ")
	inA2 := tview.NewInputField().SetLabel("Chest avg: ")
	inA3 := tview.NewInputField().SetLabel("Legs avg: ")

	btnFetch := tview.NewButton("Fetch (F)")
	btnLoad := tview.NewButton("Load (L)")
	btnSave := tview.NewButton("Save (S)")
	btnQuit := tview.NewButton("Quit (Q)")

	// --- helpers ---
	setStatus := func(msg string) { status.SetText(msg) }

	styleInput := func(in *tview.InputField) {
		in.SetFieldBackgroundColor(tcell.ColorBlack)
		in.SetFieldTextColor(tcell.ColorWhite)
		in.SetLabelColor(tcell.ColorGray)
		in.SetBackgroundColor(tcell.ColorBlack) // affects surrounding primitive area
	}

	styleButton := func(b *tview.Button) {
		b.SetLabelColor(tcell.ColorWhite)
		b.SetLabelColorActivated(tcell.ColorWhite)
		b.SetBackgroundColor(tcell.ColorBlack)
	}

	styleInput(inShale)
	styleInput(inShard)
	styleInput(inA1)
	styleInput(inA2)
	styleInput(inA3)
	styleButton(btnFetch)
	styleButton(btnLoad)
	styleButton(btnSave)
	styleButton(btnQuit)

	refresh := func() {
		rep := ComputeReport(state)
		header.SetText(fmt.Sprintf("OathPlate Calculator %s — %s", rep.Version, strings.ToUpper(rep.Mode)))
		results.SetText(RenderReportString(rep))

		if !state.FetchedAt.IsZero() {
			age := time.Since(state.FetchedAt)
			setStatus(fmt.Sprintf("Fetched: %s | Age: %s | TTL: 20m",
				state.FetchedAt.Local().Format("2006-01-02 15:04:05"),
				roundDuration(age),
			))
		} else {
			setStatus("Manual state (no fetch time)")
		}

		// keep inputs in sync with state (avg)
		inShale.SetText(fmt.Sprintf("%d", state.Shale.Avg))
		inShard.SetText(fmt.Sprintf("%d", state.Shard.Avg))
		if len(state.Armors) >= 3 {
			inA1.SetText(fmt.Sprintf("%d", state.Armors[0].Price.Avg))
			inA2.SetText(fmt.Sprintf("%d", state.Armors[1].Price.Avg))
			inA3.SetText(fmt.Sprintf("%d", state.Armors[2].Price.Avg))
		}
	}

	apply := func(field, text string) {
		v, err := parseGP(text)
		if err != nil {
			setStatus(fmt.Sprintf("[red]Invalid value[-] for %s. Use 125k, 1.25m, 1,250,000.", field))
			return
		}
		if err := ApplyManualSet(&state, field, v); err != nil {
			setStatus(fmt.Sprintf("[red]Set failed[-]: %v", err))
			return
		}
		state.Mode = "manual"
		refresh()
		setStatus(fmt.Sprintf("[green]Updated[-] %s", field))
	}

	// Enter-to-apply
	inShale.SetDoneFunc(func(k tcell.Key) {
		if k == tcell.KeyEnter {
			apply("shale.avg", inShale.GetText())
		}
	})
	inShard.SetDoneFunc(func(k tcell.Key) {
		if k == tcell.KeyEnter {
			apply("shard.avg", inShard.GetText())
		}
	})
	inA1.SetDoneFunc(func(k tcell.Key) {
		if k == tcell.KeyEnter {
			apply("armor1.avg", inA1.GetText())
		}
	})
	inA2.SetDoneFunc(func(k tcell.Key) {
		if k == tcell.KeyEnter {
			apply("armor2.avg", inA2.GetText())
		}
	})
	inA3.SetDoneFunc(func(k tcell.Key) {
		if k == tcell.KeyEnter {
			apply("armor3.avg", inA3.GetText())
		}
	})

	// actions
	doFetch := func() {
		setStatus("Fetching...")
		go func() {
			s, err := FetchStateFromAPI()
			app.QueueUpdateDraw(func() {
				if err != nil {
					setStatus(fmt.Sprintf("[red]Fetch failed[-]: %v", err))
					return
				}
				state = s
				_ = saveCache(state)
				setStatus("[green]Fetched and cached.[-]")
				refresh()
			})
		}()
	}

	doLoad := func() {
		if c, ok := loadCache(); ok {
			state = c.State
			setStatus("[green]Loaded cache.[-]")
			refresh()
		} else {
			setStatus("[red]No cache found.[-]")
		}
	}

	doSave := func() {
		if err := saveCache(state); err != nil {
			setStatus(fmt.Sprintf("[red]Save failed[-]: %v", err))
			return
		}
		setStatus("[green]Saved cache.[-]")
	}

	doQuit := func() { app.Stop() }

	btnFetch.SetSelectedFunc(doFetch)
	btnLoad.SetSelectedFunc(doLoad)
	btnSave.SetSelectedFunc(doSave)
	btnQuit.SetSelectedFunc(doQuit)

	// --- layout ---
	left := tview.NewFlex()
	left.SetDirection(tview.FlexRow)
	left.SetBorder(true)
	left.SetTitle("Inputs")

	left.AddItem(help, 1, 0, false)
	left.AddItem(inShale, 1, 0, true)
	left.AddItem(inShard, 1, 0, false)
	left.AddItem(inA1, 1, 0, false)
	left.AddItem(inA2, 1, 0, false)
	left.AddItem(inA3, 1, 0, false)
	left.AddItem(tview.NewBox(), 1, 0, false) // spacer
	left.AddItem(btnFetch, 1, 0, false)
	left.AddItem(btnLoad, 1, 0, false)
	left.AddItem(btnSave, 1, 0, false)
	left.AddItem(btnQuit, 1, 0, false)

	body := tview.NewFlex()
	body.AddItem(left, 0, 1, true)
	body.AddItem(results, 0, 2, false)

	root := tview.NewFlex()
	root.SetDirection(tview.FlexRow)
	root.AddItem(header, 1, 0, false)
	root.AddItem(body, 0, 1, true)
	root.AddItem(status, 1, 0, false)

	// global hotkeys
	root.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Rune() {
		case 'q', 'Q':
			doQuit()
			return nil
		case 'f', 'F':
			doFetch()
			return nil
		case 'l', 'L':
			doLoad()
			return nil
		case 's', 'S':
			doSave()
			return nil
		}
		return ev
	})

	refresh()
	return app.SetRoot(root, true).Run()
}
