# moneylover: write transactions to Money Lover

Go CLI around the Money Lover web API. Use it whenever the user wants a
spending or income entry recorded in Money Lover. Never re-scrape the site or
guess ids; the tool resolves names to ids itself.

Sibling: `~/personal/finance` reads a CSV export for analysis. This project is
the write side.

## Commands

Run from the project directory. `go build -o moneylover .` once, then:

- `./moneylover help` - full manual (unix man style), read it first
- `./moneylover wallets` - `id  name  balance`, one per line
- `./moneylover categories [expense|income]` - `id  type  name`, one per line
- `./moneylover add -c CATEGORY -a AMOUNT [-w WALLET] [-n NOTE] [-d YYYY-MM-DD]`
  prints `id  date  wallet  category  amount  note`
- `./moneylover list [-w WALLET] [-from DATE] [-to DATE]` - newest first,
  `id  date  category  amount  note`, expense amounts carry a `-`
- `./moneylover edit ID [-c CATEGORY] [-a AMOUNT] [-n NOTE] [-d DATE]` - change
  only the given fields, keeps event, people, image, exclude flag, reminder
- `./moneylover export [-from DATE] [-to DATE]` - CSV of every wallet in Money
  Lover's export layout. This is how `~/personal/finance` gets fresh data:
  `./moneylover export -from <last date in its store> > /tmp/ml.csv && cd ~/personal/finance && deno task import /tmp/ml.csv`
- `./moneylover search TEXT [-from DATE] [-to DATE]` - every wallet, note or
  category contains TEXT, oldest first: `id  date  wallet  category  amount  note`.
  He marks places he liked with ⭐ in the note, so `search ⭐` is his
  favourites list; `search gandhul` finds every time he ate nasi gandhul.
  Use this for any question about his past spending or places. It pulls the
  whole ledger (10 to 20s), fine.
- `./moneylover move SRC_WALLET DST_WALLET [-limit N]` - re-home every row of
  one wallet into another: re-add in the target (same date and fields, new
  id) then delete the original. Used once (Nov 2026) to fold the archived
  Wallzet cash wallet into Danamon; the API refuses a wallet change on edit.
- `./moneylover rm ID`
- `go test ./...` - self-check for amount parsing, category matching, envelope

## Input conventions (learned from his history, Nov 2025 to Nov 2026)

Two wallets. `danamon` is IDR and takes almost everything. `wise` is USD:
the amount is in dollars and the note always carries the rupiah figure
(`... Rp1,968,977`). Travel, gold and software paid by Wise card go there.

Note style: lowercase, short, `#venue` tag first, items comma separated with
`k` prices. Only airport/station codes, currency codes and `Rp`/`$` amounts
keep capitals. Dates in notes are d/m, times hh.mm. Transaction date is the
day the money left, except lodging, which is dated at check-in. The whole
ledger was normalised to these templates on 2026-11-04, so new rows must
match them or they stand out. A template only sets order and casing: every
detail he gives (converted price, exchange rate, baggage, who it was for,
a remark) stays in the note, after a comma at the end if it fits nowhere
else. Wise rows always keep the rupiah figure.

| Type | Category | Note template | Example |
|---|---|---|---|
| receipt, restaurant, warung | `a. Makan` | `#venue item 12k, item 30k` | `#dhapukupi nasi telur ayam, 4x kue, teh` |
| cafe, bakery, coffee | `a. Ngopey!` | same | `#kopikar coklat, pain au` |
| grab, gojek ride | `a. Transport Grab/Gojek` | usually empty | |
| e-money / ovo top up, airport ride, bus | `a. Transport Grab/Gojek` | `topup ovo 100k`, `topup e-money`, `ke bandara`, `kereta bandara - duri` | |
| flight | `1. Basic (primary)` | `tiket pesawat FROM - TO airline [d/m] [hh.mm][, extra]`, airport codes (BTJ, CGK, HLP, SUB, KUL, DPS, JHB, SIN), trip context last | `tiket pesawat BTJ - HLP super air jet 7/11 08.00, 1 dewasa 10kg` |
| baggage | `1. Basic (primary)` | `bagasi pesawat FROM - TO [context]` | `bagasi pesawat DPS - KUL maybank marathon` |
| train (incl. woosh, airport train) | `1. Basic (primary)` | `tiket kereta from - to operator [d/m] [hh.mm][, extra]`, stations lowercase as on the ticket | `tiket kereta pasar senen - surabaya pasarturi 11/10, Rp379.5k` |
| lodging (hotel, guesthouse, airbnb, short kos) | `1. Basic (primary)` | `penginapan city place d/m - d/m[, extra]`, on wise add `total Rp... - Rp.../day` | `penginapan surabaya homey guesthouse gubeng 25/2 - 4/3` |
| monthly kos, rent, electricity | `d. Rent fee` | `kos name[, extra]`, `listrik[ kos name]` | `kos daris nakost` |
| groceries, cake or food for home | `1. Basic (primary)` | `#shop item, item` or `kue rumah #shop` | `#mako kue rumah 24k, selai jam coklat tropikana 112k` |
| for family, friends, weddings, zakat | `b. Gifts & Donations` | who or what | `sindi nikah`, `nasi rumah #kutaraja` |
| clothes, gadgets, books, luggage | `b. Personal Items` | item + brand/color | `celana adidas biru`, `koper baller + aksesoris` |
| software, licenses, subscriptions (wise) | `b. Personal Items` | product + term | `proton vpn 24 months` |
| haircut, skincare, perfume, toiletries | `e. Makeup` | `pankas[ #place]`, product | `pankas #pik`, `sunscreen labore` |
| laundry, shoe wash | `b. Londri` | empty or `londri[ #place][ 2.6kg]`, `cuci sepatu[ 2x]` | `londri #heyclean, 2x tas, 2x sepatu` |
| doctor, medicine, massage | `f. Medical Check-up` | what + where | `tht #harbun`, `#bugarrefleksi 1jam 30menit` |
| padel, run events, sports gear | `c. Fitness/Sports` | `padel venue`, item | `padel cgd airdome` |
| fuel | `c. Gas Bill` | empty or `stylo pertalite fulltank` | |
| data plan, wifi | `d. Balance/Internet Bill` | `55gb 5G`, `myrepublic wifi` | |
| car/motor service, car wash | `e. Vehicle Maintenance` | `dorsmir`, repair detail | `ban tubles belakang honda irc` |
| gold | `Investment` | `emas 20g #shop[, rate/g, $ rate, total Rp...]` | `emas 20g #tokomasitaly, 2,520/g, 17,398/$, edc +2%, total Rp51,327,000` |
| sim card, dating app, random treats | `d. Indulge` | short | `axis masa aktif 30hari` |
| parking, unknown fees | `Other Expense` | `parkir` | |

Lodging used to go to `d. Rent fee` (Feb to Mar 2026). Since Oct 2026 it is
`1. Basic (primary)` together with tickets; that is the rule now.

Not for this tool: loans, debt collection, transfers between wallets,
savings, "Adjust Balance". He does those in the app himself. Rows from
2021 to mid 2024 were his cash wallet "Wallzet", folded into Danamon on
2026-11-04 (same dates); the wallet itself is gone.

## Answering questions

"Where did I eat in Surabaya", "which cafes did I star", "how much did I
spend on padel": `search` with the keyword, then summarise the rows. Do not
scrape the web app and do not ask him for a CSV export.

## How to add

1. Receipt photo: parse into the one-line form `#brand item 43k, item 30k`,
   category `a. Makan` or `a. Ngopey!`, amount = receipt total.
2. Booking screenshot or text (flight, train, hotel, kos, gold, purchase):
   pick the row above, build the note from its template, amount = what was
   paid. If paid in USD use `-w wise -a <dollars>` and put the rupiah in the
   note.
3. Date defaults to today. Pass `-d` for a lodging check-in or a receipt from
   another day.
4. One `add` per receipt or booking. Report the printed line, nothing else.

## Semantics

- Amount is always positive. Expense vs income comes from the category type.
- `-a 43k` means 43000. Money Lover stores plain numbers, the tool prints
  them with `.` thousands separators; USD keeps two decimals.
- `edit` is a full replace on the API side. The tool re-reads the row from
  `transaction/list-all` and resends every field, so only pass what changes.
- The API stalls now and then (every call times out for minutes). Reads
  retry 4 times with backoff; writes never retry (a retry could double-post).
  If a write fails, check with `list`/`search` before sending it again.
- `export` IDs are the first 12 hex digits of the Money Lover id as an
  integer, stable across runs, so the finance store dedupes correctly.
- Wallet matches by case-insensitive name prefix (`-w danamon`) or id.
  Defaults to `$MONEYLOVER_WALLET`, or the only wallet when there is one.
- A wrong category id makes the API hang instead of erroring; the tool times
  out at 30s and nothing is written. Use names, not ids copied from elsewhere.

## Auth

- `MONEYLOVER_EMAIL` and `MONEYLOVER_PASSWORD` are read by `login` only.
- Token cache: `~/.config/moneylover/token.json` (override with
  `MONEYLOVER_TOKEN_FILE`). Access tokens expire; the tool refreshes them
  with the cached refresh token and rewrites the file.
- `login` registers a new device (limit 5). Run it only when a command says
  `run: moneylover login`.
- Never print the token file or env values in a reply.

## Files

- `main.go` - CLI, flag parsing, name matching, output
- `api.go` - HTTP client: login, refresh, wallet/category/transaction calls
  (web API has no event, budget or campaign endpoints; those 404;
  `wallet/delete` takes `{_id, delTransactionRelated}` and is not wrapped)
- `main_test.go` - self-check
