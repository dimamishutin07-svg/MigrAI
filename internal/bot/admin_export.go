package bot

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
)

func escapeXML(s string) string { var b bytes.Buffer; xml.EscapeText(&b, []byte(s)); return b.String() }

// Inline strings preserve phone numbers and prevent spreadsheet formula execution.
func exportXLSX(rows [][]string) ([]byte, error) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	files := map[string]string{
		"[Content_Types].xml":        `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`,
		"_rels/.rels":                `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Пользователи" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
	}
	var s strings.Builder
	s.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><sheetData>`)
	for i, row := range rows {
		fmt.Fprintf(&s, `<row r="%d">`, i+1)
		for j, v := range row {
			if len([]rune(v)) > 16000 {
				v = string([]rune(v)[:16000]) + "… (полный портрет доступен в панели)"
			}
			fmt.Fprintf(&s, `<c r="%c%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, 'A'+j, i+1, escapeXML(v))
		}
		s.WriteString(`</row>`)
	}
	s.WriteString(`</sheetData></worksheet>`)
	files["xl/worksheets/sheet1.xml"] = s.String()
	for name, body := range files {
		f, e := z.Create(name)
		if e != nil {
			return nil, e
		}
		if _, e = f.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>` + body)); e != nil {
			return nil, e
		}
	}
	if e := z.Close(); e != nil {
		return nil, e
	}
	return b.Bytes(), nil
}
func (a *Admin) export(w http.ResponseWriter, r *http.Request) {
	items, total, err := a.Store.AdminUsers(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("stage"), 0, 10001)
	if err != nil {
		http.Error(w, "Ошибка выгрузки", 500)
		return
	}
	if total > 10000 {
		http.Error(w, "Более 10 000 пользователей. Уточните фильтры.", 400)
		return
	}
	rows := [][]string{{"Telegram ID", "Имя", "Ник", "ID чата", "Телефон", "Telegram", "Первое обращение", "Последняя активность", "Этап", "Портрет", "Последний вопрос (кратко)", "Последний ответ (кратко)", "Отзыв", "Согласие на связь"}}
	stages := map[string]string{"survey": "Анкета", "review": "Проверяет портрет", "ready": "Портрет готов", "chat": "Обсуждает маршруты"}
	for _, raw := range items {
		var p map[string]json.RawMessage
		if json.Unmarshal(raw, &p) != nil {
			continue
		}
		value := func(k string) string { var s string; json.Unmarshal(p[k], &s); return s }
		profile := map[string]string{}
		json.Unmarshal(p["profile"], &profile)
		link := ""
		if value("username") != "" {
			link = "https://t.me/" + value("username")
		}
		consent := "Нет"
		if string(p["contact_consent"]) == "true" {
			consent = "Да"
		}
		rows = append(rows, []string{value("id"), value("name"), value("username"), value("chat_id"), value("phone"), link, value("first_seen"), value("last_seen"), stages[value("stage")], SurveyPortrait(profile), value("last_question"), value("last_answer"), value("feedback"), consent})
	}
	data, err := exportXLSX(rows)
	if err != nil {
		http.Error(w, "Ошибка выгрузки", 500)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="migrai-users.xlsx"`)
	w.Write(data)
}
