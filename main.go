package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	shardsNeeded = 450
	shaleNeeded  = 2520
	taxRate      = 0.02

	cacheFile = "prices_cache.json"
	cacheTTL  = 20 * time.Minute
)

const (
	itemIDShale = 30848
	itemIDShard = 30765
	itemIDSale  = 30753
)

type LatestResponse struct {
	Data map[string]struct {
		High *int64 `json:"high"`
		Low  *int64 `json:"low"`
	} `json:"data"`
}

type Prices struct {
	Shale int64 `json:"shale"`
	Shard int64 `json:"shard"`
	Sale  int64 `json:"sale"`
}

type PriceCache struct {
	Prices    Prices    `json:"prices"`
	FetchedAt time.Time `json:"fetched_at"`
}

func main() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Oathplate Profit CLI")
	fmt.Println("Inputs accept: 125k, 1.25m, 2b, 1,250,000")
	fmt.Println()

	// Show cache status (informational)
	if c, ok := loadCache(); ok {
		age := time.Since(c.FetchedAt)
		if age <= cacheTTL {
			fmt.Printf("Cache available (fresh, age: %s)\n", roundDuration(age))
			printPrices(c.Prices, c.FetchedAt)
		} else {
			fmt.Printf("Cache available (stale, age: %s; TTL: %s)\n", roundDuration(age), roundDuration(cacheTTL))
			printPrices(c.Prices, c.FetchedAt)
		}
		fmt.Println()
	} else {
		fmt.Println("No cache file found.")
		fmt.Println()
	}

	choice := startupMenu(reader)

	var (
		p         Prices
		fetchedAt time.Time
	)

	switch choice {
	case "input":
		p = manualInput(reader)
		fetchedAt = time.Time{}
	case "fetch":
		c, err := refreshAll()
		if err != nil {
			fmt.Println("Fetch failed:", err)
			fmt.Println("Falling back to manual input.")
			p = manualInput(reader)
			fetchedAt = time.Time{}
		} else {
			p = c.Prices
			fetchedAt = c.FetchedAt
			fmt.Println("Prices refreshed and cached.")
			printPrices(p, fetchedAt)
		}
	case "quit":
		return
	}

	printCalc(p)

	for {
		fmt.Println()
		cmdLine := readLine(reader, "Command (fetch, calc, show, set <field> <value>, quit): ")
		cmdLine = strings.TrimSpace(cmdLine)
		if cmdLine == "" {
			continue
		}

		parts := strings.Fields(cmdLine)
		switch strings.ToLower(parts[0]) {
		case "quit", "exit", "q":
			return

		case "show":
			printPrices(p, fetchedAt)

		case "calc":
			printCalc(p)

		case "fetch":
			c, err := refreshAll()
			if err != nil {
				fmt.Println("Fetch failed:", err)
			} else {
				p = c.Prices
				fetchedAt = c.FetchedAt
				fmt.Println("Prices refreshed and cached.")
				printPrices(p, fetchedAt)
			}

		case "set":
			if len(parts) < 3 {
				fmt.Println("Usage: set shale|shard|sale 125k")
				continue
			}
			field := strings.ToLower(parts[1])
			valStr := strings.Join(parts[2:], "")
			val, err := parseGP(valStr)
			if err != nil || val < 0 {
				fmt.Println("Invalid value. Example: 125k or 1.25m")
				continue
			}
			switch field {
			case "shale":
				p.Shale = val
			case "shard":
				p.Shard = val
			case "sale":
				p.Sale = val
			default:
				fmt.Println("Unknown field. Use shale, shard, or sale.")
				continue
			}
			fmt.Println("Updated (manual override; cache timestamp unchanged).")
			printPrices(p, fetchedAt)

		default:
			fmt.Println("Unknown command.")
		}
	}
}

func startupMenu(reader *bufio.Reader) string {
	for {
		fmt.Println("Startup Menu")
		fmt.Println("  1) Manual input")
		fmt.Println("  2) Fetch prices (and cache)")
		fmt.Println("  3) Quit")
		fmt.Println()

		line := readLine(reader, "Choose 1/2/3 or type input/fetch/quit: ")
		line = strings.ToLower(strings.TrimSpace(line))

		switch line {
		case "1", "input":
			return "input"
		case "2", "fetch":
			return "fetch"
		case "3", "quit", "exit", "q":
			return "quit"
		default:
			fmt.Println("Invalid choice.")
			fmt.Println()
		}
	}
}

func manualInput(reader *bufio.Reader) Prices {
	return Prices{
		Shale: readGP(reader, "What is the cost of Infernal Shale? "),
		Shard: readGP(reader, "What is the cost of Oathplate Shards? "),
		Sale:  readGP(reader, "What is the highest oathplate armor piece's price? "),
	}
}

func refreshAll() (PriceCache, error) {
	if itemIDShale == 0 || itemIDShard == 0 || itemIDSale == 0 {
		return PriceCache{}, fmt.Errorf("set item IDs first (itemIDShale/itemIDShard/itemIDSale)")
	}

	shale, err := fetchLatestHigh(itemIDShale)
	if err != nil {
		return PriceCache{}, fmt.Errorf("shale fetch: %w", err)
	}
	shard, err := fetchLatestHigh(itemIDShard)
	if err != nil {
		return PriceCache{}, fmt.Errorf("shard fetch: %w", err)
	}
	sale, err := fetchLatestHigh(itemIDSale)
	if err != nil {
		return PriceCache{}, fmt.Errorf("sale fetch: %w", err)
	}

	c := PriceCache{
		Prices: Prices{
			Shale: shale,
			Shard: shard,
			Sale:  sale,
		},
		FetchedAt: time.Now(),
	}

	_ = saveCache(c)
	return c, nil
}

func fetchLatestHigh(id int) (int64, error) {
	url := fmt.Sprintf("https://prices.runescape.wiki/api/v1/osrs/latest?id=%d", id)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "oathplate-profit-cli/1.0 (manual refresh)")

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("bad status: %s", resp.Status)
	}

	var out LatestResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}

	key := strconv.Itoa(id)
	row, ok := out.Data[key]
	if !ok || row.High == nil {
		return 0, fmt.Errorf("no high price returned for id=%d", id)
	}
	return *row.High, nil
}

func loadCache() (PriceCache, bool) {
	b, err := os.ReadFile(cacheFile)
	if err != nil {
		return PriceCache{}, false
	}
	var c PriceCache
	if err := json.Unmarshal(b, &c); err != nil {
		return PriceCache{}, false
	}
	if c.FetchedAt.IsZero() {
		return PriceCache{}, false
	}
	return c, true
}

func saveCache(c PriceCache) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cacheFile, b, 0o644)
}

func printCalc(p Prices) {
	ingredientCost := int64(shaleNeeded)*p.Shale + int64(shardsNeeded)*p.Shard

	taxPaid := int64(float64(p.Sale) * taxRate)
	netAfterTax := p.Sale - taxPaid
	profit := netAfterTax - ingredientCost

	fmt.Println()
	fmt.Printf("If all ingredients are bought... the cost is: %s gp\n", comma(ingredientCost))
	fmt.Printf("When sold for %s gp, %s gp will be paid in taxes (2%%).\n", comma(p.Sale), comma(taxPaid))
	fmt.Printf("Net after tax: %s gp\n", comma(netAfterTax))

	if profit >= 0 {
		fmt.Printf("Profit: %s gp\n", comma(profit))
	} else {
		fmt.Printf("Loss: %s gp\n", comma(-profit))
	}

	breakEvenSale := requiredSalePrice(ingredientCost, 0)
	oneMilProfitSale := requiredSalePrice(ingredientCost, 1_000_000)

	fmt.Println()
	fmt.Printf("To break even, you must make a sale for: %s gp (tax included)\n", comma(breakEvenSale))
	fmt.Printf("To make 1,000,000 gp profit on this transaction, you must make a sale for: %s gp (tax included)\n", comma(oneMilProfitSale))
}

func printPrices(p Prices, fetchedAt time.Time) {
	if fetchedAt.IsZero() {
		fmt.Printf("shale=%s gp | shard=%s gp | sale=%s gp | fetched_at=manual\n",
			comma(p.Shale), comma(p.Shard), comma(p.Sale))
		return
	}
	age := time.Since(fetchedAt)
	fmt.Printf("shale=%s gp | shard=%s gp | sale=%s gp | fetched_at=%s (age %s)\n",
		comma(p.Shale), comma(p.Shard), comma(p.Sale),
		fetchedAt.Local().Format("2006-01-02 15:04:05"),
		roundDuration(age),
	)
}

func readGP(reader *bufio.Reader, prompt string) int64 {
	for {
		line := readLine(reader, prompt)
		value, err := parseGP(line)
		if err != nil || value < 0 {
			fmt.Println("Please enter a valid GP amount (supports k, m, b).")
			continue
		}
		return value
	}
}

func readLine(reader *bufio.Reader, prompt string) string {
	fmt.Print(prompt)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

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

func requiredSalePrice(cost int64, desiredProfit int64) int64 {
	targetNet := cost + desiredProfit
	raw := float64(targetNet) / (1.0 - taxRate)

	sale := int64(raw)
	if float64(sale) < raw {
		sale++
	}
	return sale
}

func comma(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre == 0 {
		pre = 3
	}
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
