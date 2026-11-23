package bot
import("context";"encoding/json";"io";"net/http";"strings";"testing")
type surveyTransport func(*http.Request)(*http.Response,error)
func(f surveyTransport)RoundTrip(r *http.Request)(*http.Response,error){return f(r)}
func TestSurveyKeyboardAndAPIErrors(t *testing.T){
 for _,ok:=range []bool{true,false}{
  tg:=NewTelegram("test")
  tg.client.Transport=surveyTransport(func(r *http.Request)(*http.Response,error){
   var p struct{Text string `json:"text"`; Markup struct{Keyboard [][]map[string]string `json:"keyboard"`} `json:"reply_markup"`}
   if e:=json.NewDecoder(r.Body).Decode(&p);e!=nil{t.Fatal(e)}
   if p.Text!="Гражданство?" || len(p.Markup.Keyboard)!=1 || p.Markup.Keyboard[0][0]["text"]!="Россия"{t.Fatal("bad keyboard payload")}
   body:=`{"ok":false}`;if ok{body=`{"ok":true}`}
   return &http.Response{StatusCode:200,Body:io.NopCloser(strings.NewReader(body))},nil
  })
  err:=tg.SendChoices(context.Background(),1,"Гражданство?",[]string{"Россия","Другая страна"})
  if (err==nil)!=ok{t.Fatal("API rejection lost")}
 }
}
