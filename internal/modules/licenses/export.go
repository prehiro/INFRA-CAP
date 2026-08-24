package licenses

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/xuri/excelize/v2"
)

// exportExcel streams the filtered license list as .xlsx (excelize).
func (m *Module) exportExcel(w http.ResponseWriter, r *http.Request) {
	f := filterFromRequest(r)
	items, err := m.Store.ListAllFiltered(r.Context(), f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	file := excelize.NewFile()
	sheet := "Licenses"
	file.SetSheetName("Sheet1", sheet)

	headers := []string{"Maker", "Software Name", "Version", "License Key", "Activation Key",
		"Assigned To", "Device Hostname", "Device SN", "Section", "PO No", "Status",
		"Activated On", "Expiry Date"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		file.SetCellValue(sheet, cell, h)
	}
	// header style: bold
	bold, _ := file.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	file.SetCellStyle(sheet, "A1", cellName(len(headers), 1), bold)
	// freeze header row
	file.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2"})

	for row, l := range items {
		col := 1
		put := func(v any) {
			cell, _ := excelize.CoordinatesToCellName(col, row+2)
			file.SetCellValue(sheet, cell, v)
			col++
		}
		put(l.Maker)
		put(l.SoftwareName)
		put(deref(l.Version))
		put(deref(l.AssignedTo))
		put(deref(l.DeviceHostname))
		put(deref(l.DeviceSN))
		put(deref(l.Section))
		put(deref(l.PONo))
		put(l.Status)
		put(dateStr(l.ActivatedOn))
		put(dateStr(l.ExpiryDate))
		put(deref(l.Remarks))
	}

	// column widths: header length vs longest value
	widths := make([]float64, len(headers))
	for i, h := range headers {
		widths[i] = float64(len(h)) + 2
	}
	for _, l := range items {
		vals := []string{l.Maker, l.SoftwareName, deref(l.Version), deref(l.LicenseKey),
			deref(l.ActivationKey), deref(l.AssignedTo), deref(l.DeviceHostname),
			deref(l.DeviceSN), deref(l.Section), deref(l.PONo), l.Status,
			dateStr(l.ActivatedOn), dateStr(l.ExpiryDate)}
		for i, v := range vals {
			if w := float64(len(v)) + 2; w > widths[i] && w <= 50 {
				widths[i] = w
			}
		}
	}
	for i, w := range widths {
		col, _ := excelize.ColumnNumberToName(i + 1)
		file.SetColWidth(sheet, col, col, w)
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=licenses-"+time.Now().Format("20060102")+".xlsx")
	file.Write(w)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func dateStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("02-01-06")
}

func cellName(col, row int) string {
	name, _ := excelize.CoordinatesToCellName(col, row)
	return name
}

var _ = fmt.Sprintf
var _ = strconv.Itoa
