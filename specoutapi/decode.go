package specoutapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
)

// Decode fills Req from the JSON body and 'query'-tagged fields from the URL.
// Pure function: no responder state involved.
func Decode[Req any](r *http.Request) (Req, error) {
	var req Req
	if strings.Contains(r.Header.Get("Content-Type"), "json") && r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				// empty body with a JSON content type: treat as absent
				fillQueryFields(&req, r.URL.Query())
				return req, nil
			}
			return req, err
		}
	}
	fillQueryFields(&req, r.URL.Query())
	return req, nil
}

func fillQueryFields(v any, q map[string][]string) error {
	rv := reflect.ValueOf(v).Elem()
	if rv.Kind() != reflect.Struct {
		return nil
	}
	t := rv.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("query")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		vals, ok := q[name]
		if !ok || len(vals) == 0 {
			continue
		}
		fv := rv.Field(i)
		switch fv.Kind() {
		case reflect.String:
			fv.SetString(vals[0])
		case reflect.Int, reflect.Int64:
			n, err := strconv.ParseInt(vals[0], 10, 64)
			if err != nil {
				return err
			}
			fv.SetInt(n)
		case reflect.Bool:
			b, err := strconv.ParseBool(vals[0])
			if err != nil {
				return err
			}
			fv.SetBool(b)
		case reflect.Slice:
			out := reflect.MakeSlice(fv.Type(), len(vals), len(vals))
			for j, val := range vals {
				ev := out.Index(j)
				switch ev.Kind() {
				case reflect.String:
					ev.SetString(val)
				case reflect.Int:
					n, _ := strconv.ParseInt(val, 10, 64)
					ev.SetInt(n)
				}
			}
			fv.Set(out)
		}
	}
	return nil
}
