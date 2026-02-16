package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	shardsNeeded = 450
	shaleNeeded  = 2520
	cacheFile    = "prices_cache.json"
	cacheTTL     = 20 * time.Minute

	version = "v1.0.0"
)

const (
	itemIDShale = 30848
	itemIDShard = 30765

	armorID1 = 30750 // Oathplate helmet
	armorID2 = 30753 // Oathplate chestplate
	armorID3 = 30756 // Oathplate legs
)

type PriceTriple struct {
	High int64 `json:"high"`
	Low  int64 `json:"low"`
	Avg  int64 `json:"avg"`
}

type ArmorOption struct {
	Name   string      `json:"name"`
	ItemID int         `json:"item_id"`
	Price  PriceTriple `json:"price"`
}

type AppState struct {
	Shale     PriceTriple   `json:"shale"`
	Shard     PriceTriple   `json:"shard"`
	Armors    []ArmorOption `json:"armors"`
	FetchedAt time.Time     `json:"fetched_at"`
	Mode      string        `json:"mode"` // "api" or "manual"
}

type CacheFile struct {
	State AppState `json:"state"`
}

type ProfitCase struct {
	SaleLabel   string // "low" | "avg" | "high"
	SalePrice   int64
	TaxPaid     int64
	NetAfterTax int64
	Profit      int64
}

type ArmorReport struct {
	Name     string
	ItemID   int
	Sale     PriceTriple
	Cases    []ProfitCase
	BestCase ProfitCase
}

type Report struct {
	Version    string
	Mode       string
	FetchedAt  time.Time
	CacheAge   time.Duration
	CacheFresh bool

	Shale PriceTriple
	Shard PriceTriple

	IngredientCost  PriceTriple
	Armors          []ArmorReport
	BestByAvgProfit ArmorReport
	BestByHighSale  ArmorReport
}

func main() {
	fmt.Printf("OathPlate Calculator %s\n", version)

	state := defaultState()
	if c, ok := loadCache(); ok {
		state = c.State
	}

	if err := RunTUI(state); err != nil {
		fmt.Println("TUI ERROR:", err)
	}
}

func defaultState() AppState {
	return AppState{
		Armors: []ArmorOption{
			{Name: "Oathplate Helmet", ItemID: armorID1},
			{Name: "Oathplate Chestplate", ItemID: armorID2},
			{Name: "Oathplate Legs", ItemID: armorID3},
		},
		Mode: "manual",
	}
}

/*
   FETCH (API) → STATE
*/

type latestResponse struct {
	Data map[string]struct {
		High *int64 `json:"high"`
		Low  *int64 `json:"low"`
	} `json:"data"`
}

func FetchStateFromAPI() (AppState, error) {
	ids := []int{itemIDShale, itemIDShard, armorID1, armorID2, armorID3}
	for _, id := range ids {
		if id == 0 {
			return AppState{}, errors.New("set item IDs first (shale/shard/armor1/armor2/armor3)")
		}
	}

	shale, err := fetchLatestTriple(itemIDShale)
	if err != nil {
		return AppState{}, fmt.Errorf("shale fetch: %w", err)
	}
	shard, err := fetchLatestTriple(itemIDShard)
	if err != nil {
		return AppState{}, fmt.Errorf("shard fetch: %w", err)
	}

	a1, err := fetchLatestTriple(armorID1)
	if err != nil {
		return AppState{}, fmt.Errorf("armor1 fetch: %w", err)
	}
	a2, err := fetchLatestTriple(armorID2)
	if err != nil {
		return AppState{}, fmt.Errorf("armor2 fetch: %w", err)
	}
	a3, err := fetchLatestTriple(armorID3)
	if err != nil {
		return AppState{}, fmt.Errorf("armor3 fetch: %w", err)
	}

	return AppState{
		Shale: shale,
		Shard: shard,
		Armors: []ArmorOption{
			{Name: "Oathplate Helmet", ItemID: armorID1, Price: a1},
			{Name: "Oathplate Chestplate", ItemID: armorID2, Price: a2},
			{Name: "Oathplate Legs", ItemID: armorID3, Price: a3},
		},
		FetchedAt: time.Now(),
		Mode:      "api",
	}, nil
}

func fetchLatestTriple(id int) (PriceTriple, error) {
	url := fmt.Sprintf("https://prices.runescape.wiki/api/v1/osrs/latest?id=%d", id)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return PriceTriple{}, err
	}
	req.Header.Set("User-Agent", "oathplate-calculator/1.0 (manual refresh)")

	resp, err := client.Do(req)
	if err != nil {
		return PriceTriple{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return PriceTriple{}, fmt.Errorf("bad status: %s", resp.Status)
	}

	var out latestResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return PriceTriple{}, err
	}

	key := strconv.Itoa(id)
	row, ok := out.Data[key]
	if !ok || row.High == nil || row.Low == nil {
		return PriceTriple{}, fmt.Errorf("missing high/low for id=%d", id)
	}

	avg := (*row.High + *row.Low) / 2
	return PriceTriple{High: *row.High, Low: *row.Low, Avg: avg}, nil
}

/*
   COMPUTE (pure) → REPORT
*/

func ComputeReport(state AppState) Report {
	var age time.Duration
	if !state.FetchedAt.IsZero() {
		age = time.Since(state.FetchedAt)
	}
	fresh := !state.FetchedAt.IsZero() && age <= cacheTTL

	ingredientCost := PriceTriple{
		Low:  int64(shaleNeeded)*state.Shale.Low + int64(shardsNeeded)*state.Shard.Low,
		Avg:  int64(shaleNeeded)*state.Shale.Avg + int64(shardsNeeded)*state.Shard.Avg,
		High: int64(shaleNeeded)*state.Shale.High + int64(shardsNeeded)*state.Shard.High,
	}

	armorReports := make([]ArmorReport, 0, len(state.Armors))
	for _, a := range state.Armors {
		armorReports = append(armorReports, computeArmor(a, ingredientCost))
	}

	bestByAvg := pickBestByAvgProfit(armorReports)
	bestByHighSale := pickBestByHighSale(armorReports)

	return Report{
		Version:         version,
		Mode:            state.Mode,
		FetchedAt:       state.FetchedAt,
		CacheAge:        age,
		CacheFresh:      fresh,
		Shale:           state.Shale,
		Shard:           state.Shard,
		IngredientCost:  ingredientCost,
		Armors:          armorReports,
		BestByAvgProfit: bestByAvg,
		BestByHighSale:  bestByHighSale,
	}
}

func computeArmor(a ArmorOption, ingredientCost PriceTriple) ArmorReport {
	cases := []ProfitCase{
		computeCase("low", a.Price.Low, ingredientCost.Low),
		computeCase("avg", a.Price.Avg, ingredientCost.Avg),
		computeCase("high", a.Price.High, ingredientCost.High),
	}

	best := cases[0]
	for _, c := range cases[1:] {
		if c.Profit > best.Profit {
			best = c
		}
	}

	return ArmorReport{
		Name:     a.Name,
		ItemID:   a.ItemID,
		Sale:     a.Price,
		Cases:    cases,
		BestCase: best,
	}
}

func computeCase(label string, salePrice int64, ingredientCost int64) ProfitCase {
	taxPaid := (salePrice * 2) / 100
	net := salePrice - taxPaid
	profit := net - ingredientCost

	return ProfitCase{
		SaleLabel:   label,
		SalePrice:   salePrice,
		TaxPaid:     taxPaid,
		NetAfterTax: net,
		Profit:      profit,
	}
}

func pickBestByAvgProfit(armors []ArmorReport) ArmorReport {
	if len(armors) == 0 {
		return ArmorReport{}
	}
	best := armors[0]
	bestAvgProfit := profitForLabel(best, "avg")
	for _, a := range armors[1:] {
		p := profitForLabel(a, "avg")
		if p > bestAvgProfit {
			best = a
			bestAvgProfit = p
		}
	}
	return best
}

func pickBestByHighSale(armors []ArmorReport) ArmorReport {
	if len(armors) == 0 {
		return ArmorReport{}
	}
	best := armors[0]
	for _, a := range armors[1:] {
		if a.Sale.High > best.Sale.High {
			best = a
		}
	}
	return best
}

func profitForLabel(a ArmorReport, label string) int64 {
	for _, c := range a.Cases {
		if c.SaleLabel == label {
			return c.Profit
		}
	}
	return math.MinInt64
}

/*
   RENDER (string)
*/

func RenderReportString(r Report) string {
	var b strings.Builder
	w := func(s string, args ...any) { b.WriteString(fmt.Sprintf(s, args...)) }

	b.WriteString(strings.Repeat("=", 64) + "\n")
	w("OathPlate Calculator %s\n", r.Version)

	if !r.FetchedAt.IsZero() {
		w("Mode: %s | Fetched: %s | Age: %s (%s)\n",
			r.Mode,
			r.FetchedAt.Local().Format("2006-01-02 15:04:05"),
			roundDuration(r.CacheAge),
			boolWord(r.CacheFresh, "fresh", "stale"),
		)
	} else {
		w("Mode: %s\n", r.Mode)
	}

	b.WriteString(strings.Repeat("-", 64) + "\n")

	b.WriteString("PRICES (high / low / avg)\n")
	w("  Infernal Shale:   %12s / %12s / %12s gp\n", comma(r.Shale.High), comma(r.Shale.Low), comma(r.Shale.Avg))
	w("  Oathplate Shards: %12s / %12s / %12s gp\n", comma(r.Shard.High), comma(r.Shard.Low), comma(r.Shard.Avg))
	b.WriteString("\n")

	b.WriteString("INGREDIENT COST (using shale+shards high/low/avg)\n")
	w("  Cost low:  %s gp\n", comma(r.IngredientCost.Low))
	w("  Cost avg:  %s gp\n", comma(r.IngredientCost.Avg))
	w("  Cost high: %s gp\n", comma(r.IngredientCost.High))
	b.WriteString(strings.Repeat("-", 64) + "\n")

	armors := append([]ArmorReport(nil), r.Armors...)
	sort.Slice(armors, func(i, j int) bool {
		return profitForLabel(armors[i], "avg") > profitForLabel(armors[j], "avg")
	})

	b.WriteString("ARMOR OPTIONS (sale high / low / avg) + profit using matching ingredient cost tier\n")
	for _, a := range armors {
		w("\n  %s\n", a.Name)
		w("    Sale:  %12s / %12s / %12s gp\n", comma(a.Sale.High), comma(a.Sale.Low), comma(a.Sale.Avg))
		for _, c := range a.Cases {
			sign := ""
			if c.Profit < 0 {
				sign = "-"
			}
			w("    Profit @ %-4s sale: %s%s gp (tax %s, net %s)\n",
				c.SaleLabel,
				sign, comma(abs(c.Profit)),
				comma(c.TaxPaid),
				comma(c.NetAfterTax),
			)
		}
	}

	b.WriteString("\n" + strings.Repeat("-", 64) + "\n")
	b.WriteString("RECOMMENDATION\n")
	w("  Best by AVG profit: %s (avg profit %s gp)\n",
		r.BestByAvgProfit.Name,
		comma(profitForLabel(r.BestByAvgProfit, "avg")),
	)
	w("  Highest sale HIGH:  %s (high sale %s gp)\n",
		r.BestByHighSale.Name,
		comma(r.BestByHighSale.Sale.High),
	)
	b.WriteString(strings.Repeat("=", 64) + "\n")

	return b.String()
}

/*
   MANUAL SET
*/

func ApplyManualSet(state *AppState, field string, val int64) error {
	parts := strings.Split(field, ".")
	target := parts[0]
	component := ""
	if len(parts) == 2 {
		component = parts[1]
	} else if len(parts) > 2 {
		return errors.New("invalid field format")
	}

	setTriple := func(t *PriceTriple) error {
		if component == "" {
			t.High, t.Low, t.Avg = val, val, val
			return nil
		}
		switch component {
		case "high":
			t.High = val
		case "low":
			t.Low = val
		case "avg":
			t.Avg = val
		default:
			return fmt.Errorf("unknown component %q (use high|low|avg)", component)
		}
		return nil
	}

	switch target {
	case "shale":
		return setTriple(&state.Shale)
	case "shard":
		return setTriple(&state.Shard)
	case "armor1", "armor2", "armor3":
		idx := map[string]int{"armor1": 0, "armor2": 1, "armor3": 2}[target]
		if len(state.Armors) < 3 {
			return errors.New("armor list not initialized")
		}
		return setTriple(&state.Armors[idx].Price)
	default:
		return errors.New("unknown field (use shale, shard, armor1, armor2, armor3)")
	}
}

/*
   CACHE
*/

func loadCache() (CacheFile, bool) {
	b, err := os.ReadFile(cacheFile)
	if err != nil {
		return CacheFile{}, false
	}
	var c CacheFile
	if err := json.Unmarshal(b, &c); err != nil {
		return CacheFile{}, false
	}
	return c, true
}

func saveCache(state AppState) error {
	c := CacheFile{State: state}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cacheFile, b, 0o644)
}

/*
   INPUT PARSING
*/

func parseGP(input string) (int64, error) {
	input = strings.TrimSpace(strings.ToLower(input))
	input = strings.ReplaceAll(input, ",", "")

	multiplier := float64(1)
	if strings.HasSuffix(input, "k") {
		multiplier = 1_000
		input = strings.TrimSuffix(input, "k")
	} else if strings.HasSuffix(input, "m") {
		multiplier = 1_000_000
		input = strings.TrimSuffix(input, "m")
	} else if strings.HasSuffix(input, "b") {
		multiplier = 1_000_000_000
		input = strings.TrimSuffix(input, "b")
	}

	number, err := strconv.ParseFloat(input, 64)
	if err != nil {
		return 0, err
	}
	return int64(math.Round(number * multiplier)), nil
}

/*
   UTILS
*/

func comma(n int64) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return sign + s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre == 0 {
		pre = 3
	}
	b.WriteString(sign)
	b.WriteString(s[:pre])
	for i := pre; i < len(s); i += 3 {
		b.WriteString(",")
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func roundDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()+0.5))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()+0.5))
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}

func boolWord(b bool, t, f string) string {
	if b {
		return t
	}
	return f
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
