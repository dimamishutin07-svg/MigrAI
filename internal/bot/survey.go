package bot

import (
	"fmt"
	"strconv"
	"strings"
)

type SurveyQuestion struct {
	Key, Section, Text string
	Options            []string
	Multi              bool
}

var SurveyQuestions = []SurveyQuestion{
	{Key: "current_country", Section: "🧭 Отправная точка", Text: "Где вы сейчас живёте?", Options: []string{"Россия", "Другая страна"}, Multi: false},
	{Key: "citizenship", Section: "🧭 Отправная точка", Text: "Какое у вас гражданство? Если несколько — напишите все страны.", Options: []string{"Россия", "Другая страна", "Несколько гражданств"}, Multi: false},
	{Key: "goal", Section: "🎯 Ваши планы", Text: "Чего хотите от переезда в первую очередь?", Options: []string{"Пожить за границей", "Переехать на несколько лет", "Получить ВНЖ", "Получить ПМЖ", "В будущем — гражданство", "Изучаю возможности"}, Multi: false},
	{Key: "timeline", Section: "🎯 Ваши планы", Text: "Когда хотелось бы переехать?", Options: []string{"В течение 3 месяцев", "Через 3–6 месяцев", "Через 6–12 месяцев", "Через 1–2 года", "Пока без срока"}, Multi: false},
	{Key: "countries", Section: "🎯 Ваши планы", Text: "Какие страны или регионы рассматриваете? Можно написать несколько.", Options: []string{"Испания", "Грузия", "Пока не выбраны", "Другая страна"}, Multi: false},
	{Key: "priorities", Section: "🎯 Ваши планы", Text: "Что важнее при выборе страны? Нажимайте несколько вариантов, затем «Готово».", Options: []string{"Простой ВНЖ", "Стоимость жизни", "Работа", "Бизнес", "Доходы", "Образование", "Климат", "Безопасность", "Медицина", "Налоги", "Гражданство", "Жизнь на английском", "Переезд с семьёй", "Русскоязычная среда"}, Multi: true},
	{Key: "age", Section: "👥 Вы и близкие", Text: "Укажите возрастную группу.", Options: []string{"До 18 лет", "18–24", "25–34", "35–44", "45–54", "55 и старше"}, Multi: false},
	{Key: "family", Section: "👥 Вы и близкие", Text: "С кем планируете переезжать?", Options: []string{"Один", "С партнёром", "С супругом / супругой", "С детьми", "Всей семьёй"}, Multi: false},
	{Key: "family_details", Section: "👥 Вы и близкие", Text: "Сколько человек едет? Если есть дети, укажите их возраст.", Options: []string{}, Multi: false},
	{Key: "employment", Section: "💼 Работа", Text: "Чем вы сейчас занимаетесь?", Options: []string{"Работаю по найму", "Предприниматель", "Самозанятый / фрилансер", "Студент", "Временно не работаю"}, Multi: false},
	{Key: "profession", Section: "💼 Работа", Text: "В какой сфере работаете или учитесь? Например, инженер-конструктор или дизайн.", Options: []string{}, Multi: false},
	{Key: "experience", Section: "💼 Работа", Text: "Сколько у вас опыта работы?", Options: []string{"Пока без опыта", "До 1 года", "1–3 года", "3–5 лет", "5–10 лет", "Более 10 лет"}, Multi: false},
	{Key: "remote", Section: "💼 Работа", Text: "Можете выполнять текущую работу удалённо?", Options: []string{"Да", "Частично", "Нет", "Сейчас не работаю"}, Multi: false},
	{Key: "work_plan", Section: "💼 Работа", Text: "Чем хотите заниматься после переезда?", Options: []string{"Местная компания", "Удалённая работа", "Фриланс", "Свой бизнес", "Обучение", "Пока не решил"}, Multi: false},
	{Key: "income", Section: "💰 Финансы", Text: "Какой месячный доход можете подтвердить? Достаточно диапазона — это поможет подобрать программы.", Options: []string{"До 1 000 €", "1 000–2 000 €", "2 000–3 000 €", "3 000–5 000 €", "Более 5 000 €", "Нерегулярный доход", "Предпочитаю не отвечать"}, Multi: false},
	{Key: "budget", Section: "💰 Финансы", Text: "Какой бюджет на сам переезд и оформление документов? Можно написать сумму в своей валюте.", Options: []string{"До 2 000 €", "2 000–5 000 €", "5 000–10 000 €", "10 000–25 000 €", "Более 25 000 €", "Пока не решил"}, Multi: false},
	{Key: "savings", Section: "💰 Финансы", Text: "Есть накопления, которые при необходимости сможете подтвердить?", Options: []string{"Да", "Частично", "Нет", "Предпочитаю не отвечать"}, Multi: false},
	{Key: "education", Section: "🎓 Образование и языки", Text: "Какое у вас образование? Если сейчас учитесь, можно указать и уже полученную степень.", Options: []string{"Среднее", "Среднее профессиональное", "Бакалавриат", "Магистратура", "Аспирантура / PhD", "Сейчас учусь"}, Multi: false},
	{Key: "study_route", Section: "🎓 Образование и языки", Text: "Готовы рассмотреть переезд через обучение?", Options: []string{"Да", "Нет", "Возможно"}, Multi: false},
	{Key: "languages", Section: "🎓 Образование и языки", Text: "Какие языки знаете и на каком уровне? Например, английский B1 или испанский — начинаю.", Options: []string{"Только русский", "Английский — базовый", "Английский — средний", "Английский — свободно"}, Multi: false},
	{Key: "learn_language", Section: "🎓 Образование и языки", Text: "Готовы учить язык страны переезда?", Options: []string{"Да", "Нет", "Зависит от страны"}, Multi: false},
	{Key: "ties", Section: "🔎 Дополнительные возможности", Text: "Есть родственники, происхождение или семейные связи с другими странами?", Options: []string{"Да", "Возможно", "Нет", "Не знаю"}, Multi: false},
	{Key: "ties_details", Section: "🔎 Дополнительные возможности", Text: "Назовите страну и характер связи. Например, бабушка родилась в другой стране.", Options: []string{}, Multi: false},
	{Key: "business", Section: "🔎 Дополнительные возможности", Text: "Есть свой бизнес или доля в компании?", Options: []string{"Да", "Нет"}, Multi: false},
	{Key: "investment", Section: "🔎 Дополнительные возможности", Text: "Готовы использовать капитал или имущество для инвестиционного маршрута?", Options: []string{"Да", "Возможно", "Нет"}, Multi: false},
	{Key: "achievements", Section: "🔎 Дополнительные возможности", Text: "Есть заметные достижения — награды, публикации, крупные проекты, управленческий или международный опыт?", Options: []string{"Да", "Нет"}, Multi: false},
	{Key: "achievements_details", Section: "🔎 Дополнительные возможности", Text: "Кратко расскажите о достижениях, которые стоит учесть.", Options: []string{}, Multi: false},
	{Key: "constraints", Section: "🧩 Важные детали", Text: "Есть обстоятельства, которые могут осложнить оформление? Можно выбрать несколько и нажать «Готово». Подробности и документы присылать не нужно.", Options: []string{"Отказы в визах / ВНЖ", "Подтверждение дохода", "Проблемы с документами", "Ограничения на выезд", "Судимость", "Ничего из перечисленного", "Предпочитаю не отвечать"}, Multi: true},
	{Key: "excluded_routes", Section: "🧩 Важные детали", Text: "Какие пути точно исключаете? Выберите несколько и нажмите «Готово».", Options: []string{"Обучение", "Трудоустройство", "Бизнес", "Инвестиции", "Digital Nomad", "Языковые курсы", "Семейные основания", "Готов рассмотреть всё"}, Multi: true},
	{Key: "service_goal", Section: "🏁 Завершаем портрет", Text: "С чем помочь вам в первую очередь?", Options: []string{"Подобрать страны", "Найти путь в конкретную страну", "Сравнить страны", "Проверить маршрут", "Пошаговый план", "Оценить возможности", "Рассчитать бюджет"}, Multi: false},
	{Key: "situation", Section: "🏁 Завершаем портрет", Text: "Что ещё важно знать о вашей ситуации? Напишите главный вопрос своими словами или пропустите.", Options: []string{}, Multi: false},
}

func surveyActive(q SurveyQuestion, p map[string]string) bool {
	switch q.Key {
	case "family_details":
		return p["family"] != "" && p["family"] != "Один" && p["family"] != "не указано"
	case "ties_details":
		return p["ties"] == "Да" || p["ties"] == "Возможно"
	case "achievements_details":
		return p["achievements"] == "Да"
	}
	return true
}
func SurveyNext(p map[string]string) string {
	for _, q := range SurveyQuestions {
		if surveyActive(q, p) && strings.TrimSpace(p[q.Key]) == "" {
			return q.Key
		}
	}
	return ""
}
func SurveyByKey(key string) (SurveyQuestion, bool) {
	for _, q := range SurveyQuestions {
		if q.Key == key {
			return q, true
		}
	}
	return SurveyQuestion{}, false
}
func SurveyPrompt(key string, p map[string]string) (string, []string) {
	q, ok := SurveyByKey(key)
	if !ok {
		return "", nil
	}
	done, total := 0, 0
	for _, v := range SurveyQuestions {
		if surveyActive(v, p) {
			total++
			if p[v.Key] != "" {
				done++
			}
		}
	}
	text := fmt.Sprintf("%s · заполнено %d из %d\n\n%s", q.Section, done, total, q.Text)
	if q.Multi && p[key] != "" {
		text += "\n\nВыбрано: " + p[key]
	}
	text += "\n\nМожно написать свой ответ."
	options := append([]string{}, q.Options...)
	if q.Multi {
		options = append(options, "Готово")
	}
	options = append(options, "Пропустить")
	return text, options
}

// One message updates one answer. Button values need no external AI request.
func SurveyAnswer(p map[string]string, key, text string) (string, string) {
	q, ok := SurveyByKey(key)
	if !ok {
		return SurveyNext(p), ""
	}
	if text == "Другая страна" || text == "Несколько гражданств" {
		return key, "Напишите название страны или несколько стран одним сообщением."
	}
	if text == "Пропустить" || text == "Предпочитаю не отвечать" {
		p[key] = "не указано"
	} else if q.Multi {
		if text != "Готово" {
			if text == "Ничего из перечисленного" || text == "Готов рассмотреть всё" {
				p[key] = text
			} else {
				if p[key] == "Ничего из перечисленного" || p[key] == "Готов рассмотреть всё" || p[key] == "не указано" {
					p[key] = ""
				}
				parts := strings.Split(p[key], "; ")
				found := false
				for i, v := range parts {
					if v == text {
						parts = append(parts[:i], parts[i+1:]...)
						found = true
						break
					}
				}
				if !found {
					parts = append(parts, text)
				}
				p[key] = strings.Trim(strings.Join(parts, "; "), "; ")
			}
			return key, ""
		}
		if p[key] == "" {
			return key, "Выберите хотя бы один вариант или нажмите «Пропустить»."
		}
	} else {
		p[key] = text
	}
	if key == "family" {
		delete(p, "family_details")
	}
	if key == "ties" {
		delete(p, "ties_details")
	}
	if key == "achievements" {
		delete(p, "achievements_details")
	}
	return SurveyNext(p), ""
}
func SurveyEditKey(text string) string {
	n, e := strconv.Atoi(strings.TrimSpace(text))
	if e != nil || n < 1 || n > len(SurveyQuestions) {
		return ""
	}
	return SurveyQuestions[n-1].Key
}

var surveyLabels = []string{"Где живёте", "Гражданство", "Цель", "Сроки", "Страны и регионы", "Приоритеты", "Возраст", "Кто переезжает", "Состав семьи", "Занятость", "Профессия", "Опыт", "Удалённая работа", "Работа после переезда", "Подтверждаемый доход", "Бюджет переезда", "Накопления", "Образование", "Переезд через учёбу", "Языки", "Изучение языка", "Связи с другими странами", "Страна и характер связи", "Бизнес", "Инвестиции", "Достижения", "Какие достижения", "Ограничения", "Исключённые маршруты", "Задача для MigrAI", "Ситуация и главный вопрос"}

func SurveyPortrait(p map[string]string) string {
	var b strings.Builder
	b.WriteString("🧭 Ваш портрет\n")
	section := ""
	for i, q := range SurveyQuestions {
		if !surveyActive(q, p) {
			continue
		}
		if section != q.Section {
			section = q.Section
			b.WriteString("\n" + section + "\n")
		}
		value := p[q.Key]
		if value == "" {
			value = "не указано"
		}
		fmt.Fprintf(&b, "%d. %s — %s\n", i+1, surveyLabels[i], value)
	}
	return b.String()
}
