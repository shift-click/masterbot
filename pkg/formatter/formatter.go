package formatter

import (
	"bytes"
	"fmt"
	"strings"
	"text/tabwriter"
)

func Prefix(emoji, text string) string {
	switch {
	case emoji == "":
		return text
	case text == "":
		return emoji
	default:
		return emoji + " " + text
	}
}

func Table(headers []string, rows [][]string) string {
	var buf bytes.Buffer
	writer := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	if len(headers) > 0 {
		fmt.Fprintln(writer, strings.Join(headers, "\t"))
	}
	for _, row := range rows {
		fmt.Fprintln(writer, strings.Join(row, "\t"))
	}

	_ = writer.Flush()
	return strings.TrimSpace(buf.String())
}

func Error(err error) string {
	if err == nil {
		return Prefix("⚠️", "알 수 없는 오류가 발생했습니다.")
	}
	return Prefix("⚠️", err.Error())
}
