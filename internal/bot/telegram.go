package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

type Telegram struct { token string; client *http.Client }
type Update struct { UpdateID int `json:"update_id"`; Message *Message `json:"message"` }
type Message struct { Text string `json:"text"`; From struct { ID int64 `json:"id"`; Username string `json:"username"` } `json:"from"`; Chat struct { ID int64 `json:"id"` } `json:"chat"` }
func NewTelegram(token string) *Telegram { return &Telegram{token:token, client:&http.Client{Timeout:40*time.Second}} }
func (t *Telegram) call(ctx context.Context, method string, payload any, out any) error {
	b, _ := json.Marshal(payload); req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+t.token+"/"+method, bytes.NewReader(b)); if err != nil { return fmt.Errorf("telegram %s request setup failed",method) }; req.Header.Set("Content-Type","application/json")
	r, err := t.client.Do(req); if err != nil { return fmt.Errorf("telegram %s request failed",method) }; defer r.Body.Close(); if r.StatusCode >= 300 { return fmt.Errorf("telegram %s: HTTP %s",method,r.Status) }; if out != nil { return json.NewDecoder(r.Body).Decode(out) }; return nil
}
func (t *Telegram) Updates(ctx context.Context, offset int) ([]Update,error) { var res struct { OK bool `json:"ok"`; Result []Update `json:"result"`; Description string `json:"description"` }; err:=t.call(ctx,"getUpdates",map[string]any{"offset":offset,"timeout":25,"allowed_updates":[]string{"message"}},&res); if err!=nil{return nil,err}; if !res.OK{return nil,fmt.Errorf("telegram: %s",res.Description)}; return res.Result,nil }
func (t *Telegram) Send(ctx context.Context, chatID int64, text string) error { return t.call(ctx,"sendMessage",map[string]any{"chat_id":chatID,"text":text,"disable_web_page_preview":true},nil) }

func (t *Telegram) SendDocument(ctx context.Context, chatID int64, filename, caption string, content []byte) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("chat_id", fmt.Sprint(chatID)); err != nil {
		return err
	}
	if caption != "" {
		if err := w.WriteField("caption", caption); err != nil {
			return err
		}
	}
	file, err := w.CreateFormFile("document", filename)
	if err != nil {
		return err
	}
	if _, err = io.Copy(file, bytes.NewReader(content)); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+t.token+"/sendDocument", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	r, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram sendDocument request failed")
	}
	defer r.Body.Close()
	if r.StatusCode >= 300 {
		return fmt.Errorf("telegram sendDocument: HTTP %s", r.Status)
	}
	return nil
}
