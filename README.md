# moneylover

Add Money Lover transactions from the shell, so the receipt bot on the VPS can
write straight into https://web.moneylover.me instead of handing back a line to
type by hand.

Money Lover has no public API. This talks to the same endpoints the web app
uses, found by reading other people's unofficial clients. It can stop working
when they change something.

## Setup

```
go build -o moneylover .
MONEYLOVER_EMAIL=you@example.com MONEYLOVER_PASSWORD=... ./moneylover login
```

Login is needed once. The token is cached in `~/.config/moneylover/token.json`
and renews itself. Each login uses one of the account's 5 device slots, so do
not script it.

Signed up with Google and have no password? Open web.moneylover.me, copy
`access_token` and `refresh_token` from the browser's localStorage, and save
them as `{"access_token": "...", "refresh_token": "..."}` in that file.

## Use

```
./moneylover wallets
./moneylover categories expense
./moneylover add -c "Food & Beverage" -a 43k -n "#touslesjour cream cheese"
./moneylover list
./moneylover rm <id>
```

`./moneylover help` prints the full manual. Set `MONEYLOVER_WALLET` to skip
`-w` on every call.
