# Oathplate Profit CLI

A Very simple Go-based command-line calculator for determining profitability of Crafting Oathplate Armor in OldSchoolRuneScape(OSRS)

This tool:
- Calculate the total ingredient cost
- apply a 2% Grand Exchange Tax
- Determines Net profit or Loss
- Calculates Break-even Sale Price
- Calculates Required Sale Price for 1million GP profit
- Supports price shorthand (eg 1.2m instead of 1,200,000, 125k insead of 125,000)
- Fetches Live prices from OSRS Wiki API 
- Caches Prices Locally for 20 Minutes
- Timestamps price Output for later reference

---

Already Have your ingredients? 
Manually calculate your profitability with price input.

---

## Requirements:
 - Go 1.20+ recommended

---

## Installation:

Clone the Repository:
```
git clone https://github.com/KRamPro/oathplateCalculator
```
run:
```
go run main.go
```
or of course download the executable.

---

## Usage:
When Launched, choose:
1) Manual Input
2) Fetch prices (and cache)
3) Quit

Then, 
- Fetch -> refreshes price from API
- calc -> Recalulate Profit
- show -> Display current prices and Timestamps
- set -> Override a price Manually
- quit -> exit program

  - Prices are stored in `prices_cache.json`
- Cache is valid for 20 minutes
- Stale cache is reported on startup
- Manual overrides do not modify cache timestamp

---

## API

Price data is fetched from:

https://prices.runescape.wiki/api/v1/osrs/latest

This tool uses only manual fetches and does not auto-poll the API.

---

## License

MIT License
