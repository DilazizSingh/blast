package load

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/dilaziz/blast/internal/model"
)

// parseCustomers reads a JSON array of customers. The second result is each
// customer's line in the file, for errors found later.
func parseCustomers(file string, data []byte) ([]model.Customer, []int, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	// A misspelt "refered" would otherwise load as false and misprice quietly.
	dec.DisallowUnknownFields()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return nil, nil, &Error{File: file, Line: 1, Msg: "expected a JSON array of customers"}
	}

	var (
		customers []model.Customer
		lines     []int
		firstSeen = map[string]int{}
	)
	for dec.More() {
		line := lineOf(data, elementStart(data, int(dec.InputOffset())))
		fail := func(msg string) ([]model.Customer, []int, error) {
			return nil, nil, &Error{File: file, Line: line, Msg: msg}
		}
		var c model.Customer
		if err := dec.Decode(&c); err != nil {
			return nil, nil, jsonError(file, data, line, err)
		}
		if err := checkCustomer(c); err != nil {
			return fail(err.Error())
		}
		if first, dup := firstSeen[c.ID]; dup {
			return fail(fmt.Sprintf("duplicate customer id %q (first on line %d)", c.ID, first))
		}
		firstSeen[c.ID] = line
		customers = append(customers, c)
		lines = append(lines, line)
	}

	if _, err := dec.Token(); err != nil {
		return nil, nil, jsonError(file, data, lineOf(data, len(data)), err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, nil, &Error{File: file, Line: lineOf(data, int(dec.InputOffset())), Msg: "unexpected content after the customer list"}
	}
	return customers, lines, nil
}

func checkCustomer(c model.Customer) error {
	if c.ID == "" {
		return errors.New(`customer has no "id"`)
	}
	if !c.Billing.Valid() {
		return fmt.Errorf(`customer %q: billing must be "monthly" or "annual", not %q`, c.ID, c.Billing)
	}
	for i, coupon := range c.Coupons {
		if slices.Contains(c.Coupons[:i], coupon) {
			return fmt.Errorf("customer %q lists coupon %q twice", c.ID, coupon)
		}
	}
	return nil
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
