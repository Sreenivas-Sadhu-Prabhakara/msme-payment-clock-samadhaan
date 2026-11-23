# MSME 45-day payment clock and Samadhaan complaint builder

A single-binary tool for a registered micro or small supplier in **India** who is waiting on money from a buyer. It puts every invoice on the statutory clock, works out the compound interest the buyer owes, and prints the paperwork — a polite reminder, a formal notice of demand, and a **MSME Samadhaan working paper** — for you to issue and file yourself.

It runs on your own machine, serves a small web UI on `127.0.0.1`, keeps everything in one local JSON file, and makes no outbound connections of any kind.

## What it does

Enter each invoice once — number, date, the day the buyer accepted the goods or services, the amount, the buyer's name and GSTIN, the payment date agreed in writing if there is one, and anything received so far. The tool then shows:

- the **appointed day** for every invoice, from the day of acceptance and the forty-five day ceiling;
- a **live clock** per invoice, counting down to that day and then up past it;
- the **statutory interest** accrued per invoice and in total, as compound interest with monthly rests;
- **printable documents**: a reminder, a notice of demand citing s.15 and s.16, and a Samadhaan working paper that assembles your Udyam number, the buyer's details, the invoice schedule, the principal and the interest onto one page, with a checklist of documents to attach.

It never sends an email, never sends a message and never files anything. It prepares documents; you issue them.

## Who it's for

Micro and small suppliers in India registered on the **Udyam** portal — job shops, fabricators, printers, consultancies, agencies, service contractors — and whoever keeps their books. Sections 15 and 16 protect micro and small enterprises only; a medium enterprise cannot rely on them.

## The regulation it implements

**The Micro, Small and Medium Enterprises Development Act 2006 — sections 15 and 16, and section 18 (MSME Samadhaan).**

Under **s.15** a buyer must pay on or before the date agreed upon in writing, and *in no case shall the period agreed upon between the supplier and the buyer in writing exceed forty-five days* from the day of acceptance or deemed acceptance. With no agreement in writing the period is fifteen days.

```
appointed day = agreed date, if one was agreed in writing
              = day of acceptance + 15 days, if not
              = day of acceptance + 45 days, if the agreed date is later than that
```

Under **s.16**, once the appointed day has passed the buyer is liable to pay *compound interest with monthly interest to the supplier on that amount from the appointed day at three times of the bank rate notified by the Reserve Bank*. Those monthly rests are implemented literally:

```
annual rate       = 3 x RBI Bank Rate
each whole month  : balance = balance x (1 + annual rate / 12)
trailing part     : balance = balance + balance x annual rate x days / 365
interest          = balance - principal
```

Monthly rests are measured from the appointed day itself, and short months are handled properly: a rest falling on 31 January lands on 28 or 29 February, never on 2 March. Section 2(b) of the Act defines the appointed day as the day immediately following the expiry of the statutory period, so interest runs from the day after the date the tool shows.

**s.18** lets a supplier refer the dispute to the Micro and Small Enterprises Facilitation Council, which is what the MSME Samadhaan portal exists for.

One line for buyers: under **s.43B(h) of the Income-tax Act 1961**, a sum payable to a micro or small enterprise beyond the s.15 time limit is allowed as a deduction only in the previous year in which it is actually paid.

### The figures used

| Figure | Value | Where it comes from |
| --- | --- | --- |
| Period with no written agreement | 15 days | s.15, MSMED Act 2006 |
| Ceiling on an agreed period | 45 days | s.15, MSMED Act 2006 |
| Interest multiple | 3 × Bank Rate | s.16, MSMED Act 2006 |
| RBI Bank Rate (default) | 6.50% a year | built-in default, **overridable in the tool** |
| Part-month day basis | 365 days | day-count convention used here |

Every one of these is a named constant in `internal/clock`. **The Bank Rate changes** whenever the Reserve Bank of India revises it: confirm the current rate, and the rate that applied over the period you are claiming for, with the RBI, and set it in the tool. Three times the Bank Rate at the default is 19.50% a year.

## Install and run

Go 1.25 or later. Standard library only — nothing to install.

```sh
git clone https://github.com/sreenivas-sadhu-prabhakara/msme-payment-clock-samadhaan.git
cd msme-payment-clock-samadhaan

# run it straight away
go run .

# or build the binary once and keep it
go build -o payment-clock .
./payment-clock

# options
./payment-clock -addr 127.0.0.1:8080 -data ./payment-clock-data.json
```

Then open <http://127.0.0.1:8080/>. Press Ctrl+C to stop.

| Flag | Default | What it does |
| --- | --- | --- |
| `-addr` | `127.0.0.1:8080` | Address to listen on. Keep it on 127.0.0.1 so the tool stays on this machine. |
| `-data` | `./payment-clock-data.json` | Where your records are kept. |

## Where your data is stored

In the one JSON file named by `-data`, on your own machine. It holds your firm's details, the Bank Rate you entered and your invoices, written atomically (temporary file, then rename) so an interrupted save cannot truncate your records. You can read and edit it in any text editor, and backing it up means copying one file.

No account, no server, no telemetry, no analytics, no fonts or scripts from anywhere else. The only network traffic is your own browser talking to the binary on 127.0.0.1.

## Tests

```sh
gofmt -l .
go vet ./...
go test ./... -count=1
go build -o payment-clock .
```

The statutory rules live in `internal/clock` as pure functions over plain values, with table-driven tests covering both limbs of s.15 and the forty-five day cap, the monthly-rest interest (zero days, one month, several months, part months, leap years and long months), rounding, Indian digit grouping, GSTIN and Udyam validation, and the content of every generated document.

## Scope and accuracy

The figures are a **working estimate from what you entered**. The appointed day can turn on whether and when a supply was accepted and on what was agreed in writing, both of which can be disputed. Interest for a part-paid invoice is computed on the principal still outstanding from the appointed day, which understates rather than overstates the claim. The Bank Rate changes, and the rate that applies is the one notified for the period concerned.

Confirm the current Bank Rate with the Reserve Bank of India, check every figure against your own books, and take professional advice before you rely on a notice of demand or make a reference to a Facilitation Council. The documents this tool prints are drafts for you to review, sign and issue; nothing is sent, emailed or filed for you.

MIT licensed.
