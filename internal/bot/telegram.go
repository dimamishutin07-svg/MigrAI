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

type Telegram struct { token string; client *http.Client; OnSent func(context.Context,int64,string) }
type Update struct { UpdateID int `json:"update_id"`; Message *Message `json:"message"` }
type Message struct {
 Text string `json:"text"`
 From struct { ID int64 `json:"id"`; Username string `json:"username"`; FirstName string `json:"first_name"`; LastName string `json:"last_name"` } `json:"from"`
 Chat struct { ID int64 `json:"id"` } `json:"chat"`
 Contact *struct { UserID int64 `json:"user_id"`; Phone string `json:"phone_number"` } `json:"contact"`
}
func NewTelegram(token string) *Telegram { return &Telegram{token:token, client:&http.Client{Timeout:40*time.Second}} }
func (t *Telegram) call(ctx context.Context, method string, payload any, out any) error {
	b, _ := json.Marshal(payload); req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+t.token+"/"+method, bytes.NewReader(b)); if err != nil { return fmt.Errorf("telegram %s request setup failed",method) }; req.Header.Set("Content-Type","application/json")
	r, err := t.client.Do(req); if err != nil { return fmt.Errorf("telegram %s request failed",method) }; defer r.Body.Close(); if r.StatusCode >= 300 { return fmt.Errorf("telegram %s: HTTP %s",method,r.Status) }; if out != nil { return json.NewDecoder(r.Body).Decode(out) }; var result struct { OK bool `json:"ok"` }; if err=json.NewDecoder(r.Body).Decode(&result);err!=nil{return fmt.Errorf("telegram %s invalid response",method)};if !result.OK{return fmt.Errorf("telegram %s rejected",method)};return nil
}
func (t *Telegram) Updates(ctx context.Context, offset int) ([]Update,error) { var res struct { OK bool `json:"ok"`; Result []Update `json:"result"`; Description string `json:"description"` }; err:=t.call(ctx,"getUpdates",map[string]any{"offset":offset,"timeout":25,"allowed_updates":[]string{"message"}},&res); if err!=nil{return nil,err}; if !res.OK{return nil,fmt.Errorf("telegram: %s",res.Description)}; return res.Result,nil }
func (t *Telegram) sendText(ctx context.Context,id int64,text string,markup any)error {
 payload:=map[string]any{"chat_id":id,"text":text,"disable_web_page_preview":true}
 if markup!=nil {payload["reply_markup"]=markup}
 if err:=t.call(ctx,"sendMessage",payload,nil);err!=nil{return err}
 if t.OnSent!=nil {t.OnSent(ctx,id,text)}
 return nil
}
func(t *Telegram) Send(ctx context.Context,id int64,text string)error{return t.sendText(ctx,id,text,nil)}
func(t *Telegram) RequestContact(ctx context.Context,id int64)error{
 markup:=map[string]any{"keyboard":[][]map[string]any{{{"text":"Поделиться моим номером","request_contact":true}},{{"text":"Отмена"}}},"resize_keyboard":true,"one_time_keyboard":true}
 return t.sendText(ctx,id,"Если хотите, чтобы команда связалась с вами за обратной связью, поделитесь своим номером. Это добровольно.",markup)
}

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

// Reply keyboards allow both one-tap answers and ordinary text messages.
func (t *Telegram) SendChoices(ctx context.Context, chatID int64, text string, choices []string) error {
 var keyboard [][]map[string]string
 for i:=0;i<len(choices);i+=2 {
  row:=[]map[string]string{{"text":choices[i]}}
  if i+1<len(choices){row=append(row,map[string]string{"text":choices[i+1]})}
  keyboard=append(keyboard,row)
 }
 var markup any=map[string]any{"remove_keyboard":true}
 if len(keyboard)>0 {markup=map[string]any{"keyboard":keyboard,"resize_keyboard":true,"one_time_keyboard":false,"input_field_placeholder":"Выберите вариант или напишите ответ"}}
 return t.sendText(ctx,chatID,text,markup)
}
