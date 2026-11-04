package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const usage = `NAME
    moneylover - add and read Money Lover transactions from the shell

SYNOPSIS
    moneylover login
    moneylover wallets
    moneylover categories [expense|income]
    moneylover add -c CATEGORY -a AMOUNT [-w WALLET] [-n NOTE] [-d DATE]
    moneylover list [-w WALLET] [-from DATE] [-to DATE]
    moneylover edit ID [-c CATEGORY] [-a AMOUNT] [-n NOTE] [-d DATE]
    moneylover export [-from DATE] [-to DATE]
    moneylover search TEXT [-from DATE] [-to DATE]
    moneylover move SRC_WALLET DST_WALLET [-limit N]
    moneylover rm ID

COMMANDS
    login       Sign in with MONEYLOVER_EMAIL and MONEYLOVER_PASSWORD and cache
                the token. Needed once; the token renews itself after that.
                Each login takes one of the account's 5 device slots.
    wallets     Print every wallet: id, name, balance.
    categories  Print categories the account can write to: id, type, name.
                Pass expense or income to filter.
    add         Create one transaction. Prints the new id.
    list        Print transactions of one wallet, newest first.
                Default range is the last 7 days.
    edit        Change one or more fields of a transaction. Everything else
                (event, people, image, exclude flag) is kept as it was.
    export      Print every wallet's transactions in the date range as CSV in
                Money Lover's export layout, oldest first. Default: all time.
                Feed it to ~/personal/finance with: deno task import FILE
    search      Every wallet's transactions whose note or category contains
                TEXT (case-insensitive), oldest first:
                id  date  wallet  category  amount  note. Default: all time.
                Example: moneylover search "⭐" lists the places he starred.
    move        Re-home every transaction of SRC_WALLET into DST_WALLET. The
                API refuses a wallet change on edit, so each row is re-added
                in DST (same date, category, amount, note, event, people,
                exclude flag; a new id) and the original deleted only after
                the add succeeded. Prints old_id -> new_id per row; rows
                without a category are skipped and listed on stderr.
                -limit N stops after N rows (for a dry run on one row).
    rm          Delete one transaction by id.

OPTIONS
    -w WALLET    Wallet name (case-insensitive prefix) or id. Default $MONEYLOVER_WALLET.
    -c CATEGORY  Category name or id, matched case-insensitively on the name.
    -a AMOUNT    Positive number. "43k" means 43000. Expense or income comes
                 from the category type, never from the sign.
    -n NOTE      Free text, for example the receipt line.
    -d DATE      YYYY-MM-DD. Default today.

ENVIRONMENT
    MONEYLOVER_EMAIL, MONEYLOVER_PASSWORD   used by login only
    MONEYLOVER_WALLET                       default wallet name or id
    MONEYLOVER_TOKEN_FILE                   token cache, default
                                            ~/.config/moneylover/token.json

EXIT STATUS
    0 on success, 1 on any error. Errors go to stderr.

EXAMPLES
    moneylover add -c "Food & Beverage" -a 43k -n "#touslesjour cream cheese"
    moneylover list -from 2026-11-01 -to 2026-11-30
    moneylover edit web0676...199b -c "a. Ngopey!" -a 45k
    moneylover export -from 2026-07-19 > /tmp/ml.csv
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
	switch cmd {
	case "login":
		email, password := os.Getenv("MONEYLOVER_EMAIL"), os.Getenv("MONEYLOVER_PASSWORD")
		if email == "" || password == "" {
			return fmt.Errorf("set MONEYLOVER_EMAIL and MONEYLOVER_PASSWORD")
		}
		t, err := Login(email, password)
		if err != nil {
			return err
		}
		if err := SaveTokens(t); err != nil {
			return err
		}
		fmt.Println("logged in, token saved to", tokenPath())
		return nil
	case "wallets":
		c, err := NewClient()
		if err != nil {
			return err
		}
		ws, err := c.Wallets()
		if err != nil {
			return err
		}
		for _, w := range ws {
			if w.Archived {
				continue
			}
			fmt.Printf("%s\t%s\t%s\n", w.ID, w.Name, balance(w))
		}
		return nil
	case "categories":
		c, err := NewClient()
		if err != nil {
			return err
		}
		cs, err := c.Categories()
		if err != nil {
			return err
		}
		want := 0
		if len(args) > 0 {
			want = map[string]int{"income": 1, "expense": 2}[args[0]]
		}
		for _, cat := range dedupe(cs) {
			if want != 0 && cat.Type != want {
				continue
			}
			fmt.Printf("%s\t%s\t%s\n", cat.ID, typeName(cat.Type), fullName(cs, cat))
		}
		return nil
	case "add":
		fs := flag.NewFlagSet("add", flag.ContinueOnError)
		wallet := fs.String("w", os.Getenv("MONEYLOVER_WALLET"), "")
		category := fs.String("c", "", "")
		amount := fs.String("a", "", "")
		note := fs.String("n", "", "")
		date := fs.String("d", time.Now().Format("2006-01-02"), "")
		if err := fs.Parse(args); err != nil {
			return err
		}
		amt, err := parseAmount(*amount)
		if err != nil {
			return err
		}
		if _, err := time.Parse("2006-01-02", *date); err != nil {
			return fmt.Errorf("-d must be YYYY-MM-DD")
		}
		c, err := NewClient()
		if err != nil {
			return err
		}
		w, err := findWallet(c, *wallet)
		if err != nil {
			return err
		}
		cat, err := findCategory(c, *category)
		if err != nil {
			return err
		}
		id, err := c.Add(AddInput{WalletID: w.ID, CategoryID: cat.ID, Amount: amt, Note: *note, Date: *date})
		if err != nil {
			return err
		}
		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\n", id, *date, w.Name, cat.Name, formatAmount(amt), *note)
		return nil
	case "list":
		fs := flag.NewFlagSet("list", flag.ContinueOnError)
		wallet := fs.String("w", os.Getenv("MONEYLOVER_WALLET"), "")
		from := fs.String("from", time.Now().AddDate(0, 0, -7).Format("2006-01-02"), "")
		to := fs.String("to", time.Now().Format("2006-01-02"), "")
		if err := fs.Parse(args); err != nil {
			return err
		}
		c, err := NewClient()
		if err != nil {
			return err
		}
		w, err := findWallet(c, *wallet)
		if err != nil {
			return err
		}
		txs, err := c.Transactions(w.ID, *from, *to)
		if err != nil {
			return err
		}
		for i := len(txs) - 1; i >= 0; i-- {
			t := txs[i]
			sign := ""
			if t.Category.Type == 2 {
				sign = "-"
			}
			fmt.Printf("%s\t%s\t%s\t%s%s\t%s\n", t.ID, t.DisplayDate[:10], t.Category.Name, sign, formatAmount(t.Amount), t.Note)
		}
		return nil
	case "edit":
		if len(args) < 1 {
			return fmt.Errorf("usage: moneylover edit ID [-c CATEGORY] [-a AMOUNT] [-n NOTE] [-d DATE]")
		}
		fs := flag.NewFlagSet("edit", flag.ContinueOnError)
		category := fs.String("c", "", "")
		amount := fs.String("a", "", "")
		note := fs.String("n", "\x00", "")
		date := fs.String("d", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		c, err := NewClient()
		if err != nil {
			return err
		}
		txs, err := c.All()
		if err != nil {
			return err
		}
		var t *Transaction
		for i := range txs {
			if txs[i].ID == args[0] {
				t = &txs[i]
			}
		}
		if t == nil {
			return fmt.Errorf("transaction %s not found", args[0])
		}
		if *amount != "" {
			if t.Amount, err = parseAmount(*amount); err != nil {
				return err
			}
		}
		if *note != "\x00" {
			t.Note = *note
		}
		if *date != "" {
			if _, err := time.Parse("2006-01-02", *date); err != nil {
				return fmt.Errorf("-d must be YYYY-MM-DD")
			}
			t.DisplayDate = *date
		}
		want := t.Category.Name
		if *category != "" {
			want = *category
		}
		cat, err := findCategory(c, want)
		if err != nil {
			return err
		}
		if err := c.Edit(*t, cat.ID); err != nil {
			return err
		}
		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\n", t.ID, t.DisplayDate[:10], t.Account.Name, cat.Name, formatAmount(t.Amount), t.Note)
		return nil
	case "export":
		fs := flag.NewFlagSet("export", flag.ContinueOnError)
		from := fs.String("from", "0000-01-01", "")
		to := fs.String("to", "9999-12-31", "")
		if err := fs.Parse(args); err != nil {
			return err
		}
		c, err := NewClient()
		if err != nil {
			return err
		}
		txs, err := c.All()
		if err != nil {
			return err
		}
		sort.SliceStable(txs, func(i, j int) bool { return txs[i].DisplayDate < txs[j].DisplayDate })
		w := csv.NewWriter(os.Stdout)
		w.Write([]string{"ID", "Date", "Category", "Amount", "Currency", "Note", "Account", "Event", "Exclude Report"})
		for _, t := range txs {
			d := t.DisplayDate[:10]
			if d < *from || d > *to {
				continue
			}
			amt := t.Amount
			if t.Category.Type == 2 {
				amt = -amt
			}
			event := ""
			if len(t.Campaign) > 0 {
				event = t.Campaign[0]
			}
			w.Write([]string{exportID(t.ID), d[8:] + "/" + d[5:7] + "/" + d[:4], t.Category.Name,
				strconv.FormatFloat(amt, 'f', -1, 64), currencyCode(t.Account.Currency), t.Note,
				t.Account.Name, event, strconv.FormatBool(t.ExcludeReport)})
		}
		w.Flush()
		return w.Error()
	case "search":
		if len(args) < 1 {
			return fmt.Errorf("usage: moneylover search TEXT [-from DATE] [-to DATE]")
		}
		fs := flag.NewFlagSet("search", flag.ContinueOnError)
		from := fs.String("from", "0000-01-01", "")
		to := fs.String("to", "9999-12-31", "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		c, err := NewClient()
		if err != nil {
			return err
		}
		txs, err := c.All()
		if err != nil {
			return err
		}
		sort.SliceStable(txs, func(i, j int) bool { return txs[i].DisplayDate < txs[j].DisplayDate })
		q := norm(args[0])
		for _, t := range txs {
			d := t.DisplayDate[:10]
			if d < *from || d > *to || !(strings.Contains(norm(t.Note), q) || strings.Contains(norm(t.Category.Name), q)) {
				continue
			}
			sign := ""
			if t.Category.Type == 2 {
				sign = "-"
			}
			fmt.Printf("%s\t%s\t%s\t%s\t%s%s\t%s\n", t.ID, d, t.Account.Name, t.Category.Name, sign, formatAmount(t.Amount), t.Note)
		}
		return nil
	case "move":
		if len(args) < 2 {
			return fmt.Errorf("usage: moneylover move SRC_WALLET DST_WALLET [-limit N]")
		}
		fs := flag.NewFlagSet("move", flag.ContinueOnError)
		limit := fs.Int("limit", 0, "")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		c, err := NewClient()
		if err != nil {
			return err
		}
		src, err := findWallet(c, args[0])
		if err != nil {
			return err
		}
		dst, err := findWallet(c, args[1])
		if err != nil {
			return err
		}
		if src.ID == dst.ID {
			return fmt.Errorf("source and destination are the same wallet")
		}
		cats, err := c.Categories()
		if err != nil {
			return err
		}
		txs, err := c.All()
		if err != nil {
			return err
		}
		moved := 0
		for _, t := range txs {
			if t.Account.ID != src.ID {
				continue
			}
			if *limit > 0 && moved >= *limit {
				break
			}
			if t.Category.Name == "" {
				fmt.Fprintf(os.Stderr, "skip %s\t%s\t%s\t%s\tno category\n", t.ID, t.DisplayDate[:10], formatAmount(t.Amount), t.Note)
				continue
			}
			cat, err := matchCategory(dedupe(cats), t.Category.Name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "skip %s\t%s\t%v\n", t.ID, t.DisplayDate[:10], err)
				continue
			}
			event := ""
			if len(t.Campaign) > 0 {
				event = t.Campaign[0]
			}
			newID, err := c.Add(AddInput{WalletID: dst.ID, CategoryID: cat.ID, Amount: t.Amount, Note: t.Note,
				Date: t.DisplayDate[:10], Event: event, ExcludeReport: t.ExcludeReport, With: t.With})
			if err != nil {
				return fmt.Errorf("add for %s failed: %v (moved %d so far, original untouched)", t.ID, err, moved)
			}
			if err := c.Delete(t.ID); err != nil {
				return fmt.Errorf("delete of %s failed after adding %s: %v (moved %d so far; delete it by hand)", t.ID, newID, err, moved)
			}
			moved++
			fmt.Printf("%s -> %s\t%s\t%s\t%s\t%s\n", t.ID, newID, t.DisplayDate[:10], cat.Name, formatAmount(t.Amount), t.Note)
		}
		fmt.Fprintf(os.Stderr, "moved %d rows from %s to %s\n", moved, src.Name, dst.Name)
		return nil
	case "rm":
		if len(args) != 1 {
			return fmt.Errorf("usage: moneylover rm ID")
		}
		c, err := NewClient()
		if err != nil {
			return err
		}
		if err := c.Delete(args[0]); err != nil {
			return err
		}
		fmt.Println("deleted", args[0])
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	return fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
}

func parseAmount(s string) (float64, error) {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), ",", ""))
	mult := 1.0
	if strings.HasSuffix(s, "k") {
		mult, s = 1000, strings.TrimSuffix(s, "k")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("-a must be a positive amount like 43000 or 43k")
	}
	return v * mult, nil
}

func formatAmount(v float64) string {
	if v != float64(int64(v)) {
		return strconv.FormatFloat(v, 'f', 2, 64)
	}
	s := strconv.FormatFloat(v, 'f', 0, 64)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return s
}

// exportID: the finance project dedupes on an integer ID. Money Lover ids are
// 32 hex chars; the first 12 fit in a JS safe integer and are unique enough.
func exportID(id string) string {
	n, err := strconv.ParseUint(strings.TrimPrefix(id, "web")[:12], 16, 64)
	if err != nil {
		return "0"
	}
	return strconv.FormatUint(n, 10)
}

func currencyCode(id int) string {
	if c, ok := map[int]string{1: "USD", 44: "IDR"}[id]; ok {
		return c
	}
	return strconv.Itoa(id)
}

func balance(w Wallet) string {
	for _, b := range w.Balance {
		for cur, v := range b {
			f, _ := strconv.ParseFloat(v, 64)
			return cur + " " + formatAmount(f)
		}
	}
	return ""
}

func typeName(t int) string {
	if t == 1 {
		return "income"
	}
	return "expense"
}

func findWallet(c *Client, q string) (Wallet, error) {
	ws, err := c.Wallets()
	if err != nil {
		return Wallet{}, err
	}
	if q == "" && len(ws) == 1 {
		return ws[0], nil
	}
	for _, w := range ws {
		if w.ID == q || strings.HasPrefix(strings.ToLower(w.Name), strings.ToLower(q)) {
			return w, nil
		}
	}
	// archived wallets are hidden from `wallets` but still addressable
	return Wallet{}, fmt.Errorf("wallet %q not found, run: moneylover wallets", q)
}

func findCategory(c *Client, q string) (Category, error) {
	cs, err := c.Categories()
	if err != nil {
		return Category{}, err
	}
	return matchCategory(dedupe(cs), q)
}

// list-all repeats every category once per wallet with the same id.
func dedupe(cs []Category) []Category {
	seen := map[string]bool{}
	var out []Category
	for _, c := range cs {
		if !seen[c.ID] {
			seen[c.ID] = true
			out = append(out, c)
		}
	}
	return out
}

func fullName(cs []Category, c Category) string {
	for _, p := range cs {
		if c.Parent != "" && p.ID == c.Parent {
			return p.Name + " > " + c.Name
		}
	}
	return c.Name
}

// norm lowercases and collapses runs of spaces: his categories carry double spaces ("1.  Basic").
func norm(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// matchCategory: exact id, then exact name, then unique prefix, then unique substring.
func matchCategory(cs []Category, q string) (Category, error) {
	if q == "" {
		return Category{}, fmt.Errorf("-c is required, run: moneylover categories")
	}
	lq := norm(q)
	for _, cat := range cs {
		if cat.ID == q || norm(cat.Name) == lq {
			return cat, nil
		}
	}
	for _, test := range []func(string) bool{
		func(n string) bool { return strings.HasPrefix(n, lq) },
		func(n string) bool { return strings.Contains(n, lq) },
	} {
		var hits []Category
		for _, cat := range cs {
			if test(norm(cat.Name)) {
				hits = append(hits, cat)
			}
		}
		if len(hits) == 1 {
			return hits[0], nil
		}
		if len(hits) > 1 {
			names := make([]string, len(hits))
			for i, h := range hits {
				names[i] = h.Name
			}
			return Category{}, fmt.Errorf("category %q is ambiguous: %s", q, strings.Join(names, ", "))
		}
	}
	return Category{}, fmt.Errorf("category %q not found, run: moneylover categories", q)
}
