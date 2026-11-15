package load

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/dilaziz/blast/internal/model"
)

// located is a customer and the line of the file it came from.
type located struct {
	model.Customer
	line int
}

// parseCustomers reads customers from JSON, or from CSV when the file name
// ends in .csv. The second result is each customer's line in the file, for
// errors found later.
func parseCustomers(file string, data []byte) ([]model.Customer, []int, error) {
	// Spreadsheets and some editors start a file with a byte-order mark.
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	decode := decodeJSON
	if strings.EqualFold(filepath.Ext(file), ".csv") {
		decode = decodeCSV
	}
	rows, err := decode(file, data)
	if err != nil {
		return nil, nil, err
	}

	customers := make([]model.Customer, len(rows))
	lines := make([]int, len(rows))
	firstSeen := make(map[string]int, len(rows))
	for i, row := range rows {
		if err := checkCustomer(row.Customer); err != nil {
			return nil, nil, &Error{File: file, Line: row.line, Msg: err.Error()}
		}
		if first, dup := firstSeen[row.ID]; dup {
			return nil, nil, &Error{File: file, Line: row.line,
				Msg: fmt.Sprintf("duplicate customer id %q (first on line %d)", row.ID, first)}
		}
		firstSeen[row.ID] = row.line
		customers[i], lines[i] = row.Customer, row.line
	}
	return customers, lines, nil
}

func checkCustomer(c model.Customer) error {
	switch {
	case c.ID == "":
		return errors.New(`customer has no "id"`)
	case !printable(c.ID):
		// An id is printed in reports; see printable.
		return fmt.Errorf("customer id %q must be plain, visible text", c.ID)
	case !c.Billing.Valid():
		return fmt.Errorf(`customer %q: billing must be "monthly" or "annual", not %q`, c.ID, c.Billing)
	}
	for i, coupon := range c.Coupons {
		if slices.Contains(c.Coupons[:i], coupon) {
			return fmt.Errorf("customer %q lists coupon %q twice", c.ID, coupon)
		}
	}
	return nil
}

// checkQuantity validates a quantity that a file states outright. Leaving
// the field out means one; writing zero is more likely a mistake than a
// customer who should be priced as one seat.
func checkQuantity(q int64) error {
	if q < 1 || q > model.MaxQuantity {
		return fmt.Errorf("quantity must be between 1 and %s (leave it out for a single seat)", model.Thousands(model.MaxQuantity))
	}
	return nil
}

// jsonCustomer shadows Quantity with a pointer to tell "absent" from zero.
type jsonCustomer struct {
	model.Customer
	Quantity *int64 `json:"quantity"`
}

func decodeJSON(file string, data []byte) ([]located, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	// A misspelt "refered" would otherwise load as false and misprice quietly.
	dec.DisallowUnknownFields()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return nil, &Error{File: file, Line: 1, Msg: "expected a JSON array of customers"}
	}

	var rows []located
	for dec.More() {
		line := lineOf(data, elementStart(data, int(dec.InputOffset())))
		var raw jsonCustomer
		if err := dec.Decode(&raw); err != nil {
			return nil, jsonError(file, data, line, err)
		}
		c := raw.Customer
		if raw.Quantity != nil {
			if err := checkQuantity(*raw.Quantity); err != nil {
				return nil, &Error{File: file, Line: line, Msg: err.Error()}
			}
			c.Quantity = *raw.Quantity
		}
		rows = append(rows, located{c, line})
	}

	if _, err := dec.Token(); err != nil {
		return nil, jsonError(file, data, lineOf(data, len(data)), err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, &Error{File: file, Line: lineOf(data, int(dec.InputOffset())), Msg: "unexpected content after the customer list"}
	}
	return rows, nil
}

// jsonError turns a decoder error into one with a line. line is where the
// customer being decoded starts, used when the error carries no offset.
func jsonError(file string, data []byte, line int, err error) error {
	var (
		syntax *json.SyntaxError
		typ    *json.UnmarshalTypeError
		msg    = strings.TrimPrefix(err.Error(), "json: ")
	)
	switch {
	case errors.As(err, &syntax):
		line, msg = lineOf(data, int(syntax.Offset)), "invalid JSON: "+syntax.Error()
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		line, msg = lineOf(data, len(data)), "invalid JSON: the file ends early"
	case errors.As(err, &typ) && typ.Field == "":
		msg = fmt.Sprintf("each customer must be a JSON object, not a %s", typ.Value)
	case errors.As(err, &typ):
		msg = fmt.Sprintf("field %q cannot be a %s", typ.Field, typ.Value)
	}
	return &Error{File: file, Line: line, Msg: msg}
}

// elementStart skips the separators between array elements, so the offset
// lands on the element itself rather than on the comma before it.
func elementStart(data []byte, offset int) int {
	for offset < len(data) && strings.IndexByte(", \t\r\n", data[offset]) >= 0 {
		offset++
	}
	return offset
}

func lineOf(data []byte, offset int) int {
	offset = min(max(offset, 0), len(data))
	return 1 + bytes.Count(data[:offset], []byte{'\n'})
}

// csvColumns are the columns a customer CSV may have, in documentation
// order. The first three are required.
var csvColumns = []string{"id", "plan", "billing", "coupons", "referred", "quantity"}

func decodeCSV(file string, data []byte) ([]located, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if err != nil {
		if err == io.EOF {
			err = fmt.Errorf("expected a header row: %s", strings.Join(csvColumns, ","))
		}
		return nil, csvError(file, 1, err)
	}
	columns, err := csvHeader(header)
	if err != nil {
		return nil, csvError(file, 1, err)
	}

	var rows []located
	for {
		record, err := r.Read()
		if err == io.EOF {
			return rows, nil
		}
		line, _ := r.FieldPos(0)
		if err != nil {
			return nil, csvError(file, line, err)
		}
		c, err := csvCustomer(record, columns)
		if err != nil {
			return nil, csvError(file, line, err)
		}
		rows = append(rows, located{c, line})
	}
}

// csvHeader maps each column name to its position. Unknown columns are
// refused for the same reason unknown JSON fields are.
func csvHeader(header []string) (map[string]int, error) {
	columns := make(map[string]int, len(header))
	for i, h := range header {
		name := strings.ToLower(strings.TrimSpace(h))
		if !slices.Contains(csvColumns, name) {
			return nil, fmt.Errorf("unknown column %q (expected %s)", h, strings.Join(csvColumns, ", "))
		}
		if _, dup := columns[name]; dup {
			return nil, fmt.Errorf("column %q appears twice", name)
		}
		columns[name] = i
	}
	for _, required := range csvColumns[:3] {
		if _, ok := columns[required]; !ok {
			return nil, fmt.Errorf("missing column %q", required)
		}
	}
	return columns, nil
}

func csvCustomer(record []string, columns map[string]int) (model.Customer, error) {
	field := func(name string) string {
		if i, ok := columns[name]; ok {
			return strings.TrimSpace(record[i])
		}
		return ""
	}
	c := model.Customer{ID: field("id"), Plan: field("plan"), Billing: model.Billing(field("billing"))}

	// Coupons share one cell, separated by anything a spreadsheet user might reach for.
	for _, coupon := range strings.FieldsFunc(field("coupons"), func(r rune) bool { return strings.ContainsRune(";,|", r) }) {
		if coupon = strings.TrimSpace(coupon); coupon != "" {
			c.Coupons = append(c.Coupons, coupon)
		}
	}
	switch referred := strings.ToLower(field("referred")); referred {
	case "", "false", "0", "no", "n", "f":
	case "true", "1", "yes", "y", "t":
		c.Referred = true
	default:
		return c, fmt.Errorf("referred must be true or false, not %q", referred)
	}
	if q := field("quantity"); q != "" {
		n, err := strconv.ParseInt(q, 10, 64)
		if err != nil {
			return c, fmt.Errorf("quantity %q is not a whole number", q)
		}
		if err := checkQuantity(n); err != nil {
			return c, err
		}
		c.Quantity = n
	}
	return c, nil
}

// csvError gives an error a line, preferring the one the CSV parser itself
// blames when the problem is the CSV syntax.
func csvError(file string, line int, err error) error {
	var syntax *csv.ParseError
	if errors.As(err, &syntax) {
		return &Error{File: file, Line: syntax.Line, Msg: syntax.Err.Error()}
	}
	return &Error{File: file, Line: line, Msg: err.Error()}
}
