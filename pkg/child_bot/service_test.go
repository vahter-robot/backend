package child_bot

import (
	"github.com/stretchr/testify/assert"
	"sort"
	"testing"
)

func TestParseKeywordsAndModeOK(t *testing.T) {
	s := &service{
		keywordsLimitPerBot: 50,
		inLimitPerKeyword:   25,
		inLimitChars:        100,
		outLimitChars:       1000,
	}

	kws, m, ok := s.parseKeywordsAndMode(`1
===
ваканс
===
Спасибо за предложение, но я не в поиске работы
===
да
===
реклама,прайс
===
Прайс на рекламу в канале:

1) Стартапы и бизнес
100 рублей

Если цена устраивает, отправьте ссылку на ресурс который будем размещать
===
нет
===
сотруднич,партнер
===
Сотрудничество интересно, давайте обсудим
===
нет`)

	assert.Len(t, kws, 3)

	in1 := []string{"реклама", "прайс"}
	sort.Strings(in1)
	sort.Strings(kws[1].In)
	in2 := []string{"сотруднич", "партнер"}
	sort.Strings(in2)
	sort.Strings(kws[2].In)
	assert.Equal(t, []Keyword{{
		In:  []string{"ваканс"},
		Out: "Спасибо за предложение, но я не в поиске работы",
		Ban: true,
	}, {
		In: in1,
		Out: `Прайс на рекламу в канале:

1) Стартапы и бизнес
100 рублей

Если цена устраивает, отправьте ссылку на ресурс который будем размещать`,
		Ban: false,
	}, {
		In:  in2,
		Out: "Сотрудничество интересно, давайте обсудим",
		Ban: false,
	}}, kws)

	assert.Equal(t, OnlyFirst, m)
	assert.Equal(t, ok, true)
}
