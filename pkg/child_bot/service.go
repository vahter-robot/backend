package child_bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog"
	"github.com/vahter-robot/backend/pkg/child_state"
	"github.com/vahter-robot/backend/pkg/peer"
	"github.com/vahter-robot/backend/pkg/user"
	"golang.org/x/sync/errgroup"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type service struct {
	childBotHost         string
	childBotPort         string
	childTokenPathPrefix string
	childStateRepo       *child_state.Repo
	userRepo             *user.Repo
	peerRepo             *peer.Repo
	childBotRepo         *Repo
	keywordsLimit        uint16
	inLimitPerKeyword    uint16
	inLimitChars         uint16
	outLimitChars        uint16
	parentBotUsername    string
	logger               zerolog.Logger
}

func NewService(
	logger zerolog.Logger,
	childBotHost,
	childBotPort,
	childTokenPathPrefix string,
	childStateRepo *child_state.Repo,
	userRepo *user.Repo,
	peerRepo *peer.Repo,
	childBotRepo *Repo,
	keywordsLimitPerBot,
	inLimitPerKeyword,
	inLimitChars,
	outLimitChars uint16,
	parentBotUsername string,
) *service {
	return &service{
		childBotHost:         childBotHost,
		childBotPort:         childBotPort,
		childTokenPathPrefix: childTokenPathPrefix,
		childStateRepo:       childStateRepo,
		userRepo:             userRepo,
		peerRepo:             peerRepo,
		childBotRepo:         childBotRepo,
		keywordsLimit:        keywordsLimitPerBot,
		inLimitPerKeyword:    inLimitPerKeyword,
		inLimitChars:         inLimitChars,
		outLimitChars:        outLimitChars,
		parentBotUsername:    parentBotUsername,
		logger:               logger.With().Str("package", "child_bot").Logger(),
	}
}

const (
	start       = "/start"
	help        = "/help"
	getStart    = "/get_start"
	setStart    = "/set_start"
	getKeywords = "/get_keywords"
	setKeywords = "/set_keywords"

	userForward    = "👤 "
	chatForward    = "💬 "
	messageForward = "✉️ "
	mute           = "mute"

	yes   = "да"
	no    = "нет"
	delim = "==="
	comma = ","
)

func (s *service) Serve(ctx context.Context, setWebhooks bool) error {
	if setWebhooks {
		go func() {
			wh := s.childBotRepo.Get(ctx)
			for item := range wh {
				if item.Err != nil {
					s.logger.Error().Err(item.Err).Send()
					return
				}

				api, err := tgbotapi.NewBotAPI(item.Doc.Token)
				if err != nil {
					s.logger.Warn().Err(err).Send()
					continue
				}

				_, err = api.SetWebhook(tgbotapi.NewWebhook(fmt.Sprintf(
					"%s/%s/%s", s.childBotHost, s.childTokenPathPrefix, item.Doc.Token,
				)))
				if err != nil {
					s.logger.Warn().Err(err).Send()
					continue
				}

				err = s.childBotRepo.SetWebhookNow(ctx, item.Doc.ID)
				if err != nil {
					s.logger.Error().Err(err).Send()
					continue
				}
			}
		}()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write(nil)
		if err != nil {
			s.logger.Error().Err(err).Send()
		}
	})
	pathPrefix := fmt.Sprintf("/%s/", s.childTokenPathPrefix)
	mux.HandleFunc(pathPrefix, func(w http.ResponseWriter, r *http.Request) {
		rc, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		token := strings.TrimPrefix(r.URL.Path, pathPrefix)

		s.logger.Debug().Str("path", r.URL.Path).Str("token", token).Msg("got request")

		whOK, err := s.handle(rc, token, r.Body)
		if err != nil {
			s.logger.Warn().Err(err).Send()
		}

		if !whOK {
			w.WriteHeader(http.StatusNotFound)
		}
		_, err = w.Write(nil)
		if err != nil {
			s.logger.Error().Err(err).Send()
		}
	})

	srv := &http.Server{
		Addr:    net.JoinHostPort("0.0.0.0", s.childBotPort),
		Handler: mux,
	}
	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		err := srv.Shutdown(sc)
		if err != nil {
			s.logger.Error().Err(err).Send()
		}
	}()

	err := srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("srv.ListenAndServe: %w", err)
	}

	return nil
}

type update struct {
	Message message `json:"message"`
}

type message struct {
	MessageID      int64          `json:"message_id"`
	Chat           chat           `json:"chat"`
	From           from           `json:"from"`
	Text           string         `json:"text"`
	ReplyToMessage replyToMessage `json:"reply_to_message"`
}

type replyToMessage struct {
	MessageID int64  `json:"message_id"`
	From      from   `json:"from"`
	Text      string `json:"text"`
}

type chat struct {
	ID int64 `json:"id"`
}

type from struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

func (s *service) handle(ctx context.Context, token string, body io.ReadCloser) (bool, error) {
	defer func() {
		err := body.Close()
		if err != nil {
			s.logger.Error().Err(err).Send()
		}
	}()

	b, err := io.ReadAll(body)
	if err != nil {
		return true, fmt.Errorf("io.ReadAll: %w", err)
	}

	s.logger.Debug().Str("body", string(b)).Send()

	var upd update
	err = json.Unmarshal(b, &upd)
	if err != nil {
		return true, fmt.Errorf("json.Unmarshal: %w", err)
	}

	if upd.Message.Text == "" {
		return true, nil
	}

	bot, found, err := s.childBotRepo.GetByToken(ctx, token)
	if err != nil {
		return true, fmt.Errorf("s.childBotRepo.GetByToken: %w", err)
	}
	if !found {
		return false, nil
	}

	var eg errgroup.Group
	var owner user.User
	eg.Go(func() error {
		r, e := s.userRepo.GetByID(ctx, bot.OwnerUserID)
		if e != nil {
			return fmt.Errorf("s.userRepo.GetByID: %w", e)
		}
		owner = r
		return nil
	})

	var api *tgbotapi.BotAPI
	eg.Go(func() error {
		r, e := tgbotapi.NewBotAPI(bot.Token)
		if e != nil {
			return fmt.Errorf("tgbotapi.NewBotAPI: %w", e)
		}
		api = r
		return nil
	})
	err = eg.Wait()
	if err != nil {
		return true, fmt.Errorf("eg.Wait: %w", err)
	}

	switch upd.Message.From.ID {
	case owner.TgUserID:
		err = s.handleOwner(ctx, api, upd, bot, owner)
		if err != nil {
			return true, fmt.Errorf("s.handleOwner: %w", err)
		}
	default:
		err = s.handlePeer(ctx, api, upd, bot)
		if err != nil {
			return true, fmt.Errorf("s.handlePeer: %w", err)
		}
	}
	return true, nil
}

func (s *service) handleOwner(ctx context.Context, api *tgbotapi.BotAPI, upd update, bot Bot, owner user.User) error {
	err := s.childBotRepo.SetUserChatID(ctx, bot.ID, upd.Message.Chat.ID)
	if err != nil {
		return fmt.Errorf("s.childBotRepo.SetUserChatID: %w", err)
	}

	text := upd.Message.Text
	replyText := upd.Message.ReplyToMessage.Text
	if strings.HasPrefix(replyText, userForward) && upd.Message.ReplyToMessage.From.Username == api.Self.UserName {
		rows := strings.Split(replyText, "\n")
		if len(rows) >= 3 {
			uID, e := strconv.Atoi(strings.TrimPrefix(rows[0], userForward))
			if e != nil {
				return fmt.Errorf("strconv.Atoi: %w", e)
			}
			userID := int64(uID)

			cID, e := strconv.Atoi(strings.TrimPrefix(rows[1], chatForward))
			if e != nil {
				return fmt.Errorf("strconv.Atoi: %w", e)
			}
			chatID := int64(cID)

			if text == mute {
				e = s.peerRepo.CreateMuted(ctx, bot.ID, userID, chatID)
				if e != nil {
					return fmt.Errorf("s.peerRepo.CreateMuted: %w", e)
				}

				e = s.replyOK(api, upd, "Заблокирован")
				if e != nil {
					return fmt.Errorf("s.replyOK: %w", e)
				}
				return nil
			}

			messageID, e := strconv.Atoi(strings.TrimPrefix(rows[2], messageForward))
			if e != nil {
				return fmt.Errorf("strconv.Atoi: %w", e)
			}

			_, e = api.Send(tgbotapi.MessageConfig{
				BaseChat: tgbotapi.BaseChat{
					ChatID:           chatID,
					ReplyToMessageID: messageID,
				},
				Text: text,
			})
			if e != nil {
				er := s.replyErr(api, upd, "Не отправлено. Возможно пользователь остановил бота")
				if er != nil {
					return fmt.Errorf("s.replyErr: %w", er)
				}
				return nil
			}

			e = s.replyOK(api, upd, "Отправлено")
			if e != nil {
				return fmt.Errorf("s.replyOK: %w", e)
			}
		}
	}

	switch text {
	case start, setStart:
		e := s.handleOwnerStart(ctx, api, upd, bot, owner)
		if e != nil {
			return fmt.Errorf("s.handleOwnerStart: %w", e)
		}
	case help:
		e := s.handleOwnerHelp(ctx, api, upd, bot, owner)
		if e != nil {
			return fmt.Errorf("s.handleOwnerHelp: %w", e)
		}
	case setKeywords:
		e := s.handleOwnerSetKeywords(ctx, api, upd, bot, owner)
		if e != nil {
			return fmt.Errorf("s.handleOwnerSetKeywords: %w", e)
		}
	case getKeywords:
		e := s.handleOwnerGetKeywords(api, upd, bot)
		if e != nil {
			return fmt.Errorf("s.handleOwnerGetKeywords: %w", e)
		}
	case getStart:
		e := s.handleOwnerGetStart(api, upd, bot)
		if e != nil {
			return fmt.Errorf("s.handleOwnerGetStart: %w", e)
		}
	default:
		scene, e := s.childStateRepo.GetScene(ctx, owner.ID, bot.ID)
		if e != nil {
			return fmt.Errorf("s.childStateRepo.GetScene: %w", e)
		}

		switch scene {
		case child_state.SetStart:
			e = s.childBotRepo.SetOnPeerStart(ctx, bot.ID, text)
			if e != nil {
				return fmt.Errorf("s.childBotRepo.SetOnPeerStart: %w", e)
			}

			if !bot.SetupDone {
				e = s.handleOwnerSetKeywords(ctx, api, upd, bot, owner)
				if e != nil {
					return fmt.Errorf("s.handleOwnerSetKeywords: %w", e)
				}
				return nil
			}

			e = s.childStateRepo.SetScene(ctx, owner.ID, bot.ID, child_state.None)
			if e != nil {
				return fmt.Errorf("s.childBotRepo.SetOnPeerStart: %w", e)
			}

			e = s.replyOK(api, upd, "Приветственное сообщение установлено")
			if e != nil {
				return fmt.Errorf("s.replyOK: %w", e)
			}
			return nil
		case child_state.SetKeywords:
			kws, m, ok := s.parseKeywordsAndMode(text)
			if !ok {
				e = s.replyErr(api, upd, "Некорректный формат / не соблюдены лимиты. Пожалуйста, напишите "+
					"аналогично примеру")
				if e != nil {
					return fmt.Errorf("s.replyErr: %w", e)
				}
				return nil
			}

			e = s.childBotRepo.SetKeywordsAndMode(ctx, bot.ID, kws, m)
			if e != nil {
				return fmt.Errorf("s.childBotRepo.SetKeywordsAndMode: %w", e)
			}

			if !bot.SetupDone {
				e = s.childBotRepo.SetSetupDoneTrue(ctx, bot.ID)
				if e != nil {
					return fmt.Errorf("s.childBotRepo.SetSetupDoneTrue: %w", e)
				}
			}

			e = s.childStateRepo.SetScene(ctx, owner.ID, bot.ID, child_state.None)
			if e != nil {
				return fmt.Errorf("s.childBotRepo.SetOnPeerStart: %w", e)
			}

			if !bot.SetupDone {
				e = s.replyOK(api, upd, "Бот настроен и работает 👍. Опционально можно "+
					"установить аватар и описание через @BotFather")
				if e != nil {
					return fmt.Errorf("s.replyOK: %w", e)
				}
				return nil
			}

			e = s.replyOK(api, upd, "Ключевые слова установлены")
			if e != nil {
				return fmt.Errorf("s.replyOK: %w", e)
			}
			return nil
		default:
			e = s.replyErr(api, upd, "Неизвестная команда")
			if e != nil {
				return fmt.Errorf("s.replyErr: %w", e)
			}
			return nil
		}
	}
	return nil
}

func (s *service) handleOwnerHelp(
	ctx context.Context,
	api *tgbotapi.BotAPI,
	upd update,
	bot Bot,
	owner user.User,
) error {
	err := s.childStateRepo.SetScene(ctx, owner.ID, bot.ID, child_state.None)
	if err != nil {
		return fmt.Errorf("s.childStateRepo.SetScene: %w", err)
	}

	err = s.reply(api, upd, fmt.Sprintf(`Команды

%s — показать текущее приветственное сообщение бота
%s — установить его

%s — показать текущие ключевые слова и автоответы, правила бана
%s — установить их

%s — выйти из любого меню и показать это сообщение

Для создания и удаления ботов используйте @%s`,
		getStart, setStart, getKeywords, setKeywords, help, s.parentBotUsername),
	)
	if err != nil {
		return fmt.Errorf("s.reply: %w", err)
	}

	return nil
}

func (s *service) handleOwnerStart(
	ctx context.Context,
	api *tgbotapi.BotAPI,
	upd update,
	bot Bot,
	owner user.User,
) error {
	err := s.childStateRepo.SetScene(ctx, owner.ID, bot.ID, child_state.SetStart)
	if err != nil {
		return fmt.Errorf("s.childStateRepo.SetScene: %w", err)
	}

	err = s.reply(api, upd, "Какой текст бот должен отвечать "+
		"когда нажимают кнопку 'Начать'? Например: 'Привет, слушаю вас'")
	if err != nil {
		return fmt.Errorf("s.reply: %w", err)
	}

	return nil
}

func (s *service) handleOwnerSetKeywords(
	ctx context.Context,
	api *tgbotapi.BotAPI,
	upd update,
	bot Bot,
	owner user.User,
) error {
	err := s.childStateRepo.SetScene(ctx, owner.ID, bot.ID, child_state.SetKeywords)
	if err != nil {
		return fmt.Errorf("s.childStateRepo.SetScene: %w", err)
	}

	err = s.reply(api, upd, fmt.Sprintf(`Настройка правил автоответов (не более 50), отправьте все правила одним сообщением. Если сообщение не попало под правила, бот перешлет его вам (если отправитель не в бане). Формат:
- Режим работы. Если указано '1' — бот применяет правила только на первое сообщение, далее не вмешивается в вашу переписку с отправителем. Если указано '2' — бот применяет правила и на первое сообщение отправителя, и на дальнейшие;
- Перечислите через запятую ключевые слова, ожидаемые в сообщении отправителя (не более 25);
- Затем укажите автоответ, который должен отправить бот (не более 1000 символов, может быть многострочным);
- Далее напишите нужно ли банить отправителя, если данный фильтр сработал на его сообщение. Если указано 'да' – бот ответит отправителю, далее бот игнорирует любые сообщения от него, бот не пересылает вам ни первое ни последующие сообщения от данного пользователя. Если указано 'нет' — бот ответит отправителю, перешлет вам исходное сообщение и ответ на него, вы сможете вести переписку с отправителем анонимно через бота, а забанить можно ответив '%s';
- Все элементы с новой строки и разделены '==='.

Например:

1
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
нет`, mute))
	if err != nil {
		return fmt.Errorf("s.reply: %w", err)
	}

	return nil
}

func (s *service) handleOwnerGetKeywords(
	api *tgbotapi.BotAPI,
	upd update,
	bot Bot,
) error {
	var keywords string
	for _, word := range bot.Keywords {
		keywords += fmt.Sprintf(`

%s
===
%s
===
%s`, strings.Join(word.In, comma), word.Out, boolToRU(word.Ban))
	}

	err := s.reply(api, upd, fmt.Sprintf(`Ключевые слова (%d/%d). Формат:
Ключевое слово
===
Автоответ
===
Банить
%s

%s`, len(bot.Keywords), s.keywordsLimit, keywords, help))
	if err != nil {
		return fmt.Errorf("s.reply: %w", err)
	}

	return nil
}

func (s *service) handleOwnerGetStart(
	api *tgbotapi.BotAPI,
	upd update,
	bot Bot,
) error {
	err := s.reply(api, upd, fmt.Sprintf(`Приветственное сообщение бота (когда нажимают кнопку 'Начать')

%s

%s`, bot.OnPeerStart, help))
	if err != nil {
		return fmt.Errorf("s.reply: %w", err)
	}

	return nil
}

func (s *service) handlePeer(ctx context.Context, api *tgbotapi.BotAPI, upd update, bot Bot) error {
	// (1) user: /start
	// (2) bot : start reaction
	// (3) user: first real message
	if bot.Mode == OnlyFirst && upd.Message.MessageID > 3 {
		return nil
	}

	muted, err := s.peerRepo.IsMuted(ctx, bot.ID, upd.Message.From.ID)
	if err != nil {
		return fmt.Errorf("s.peerRepo.IsMuted: %w", err)
	}
	if muted {
		return nil
	}

	text := upd.Message.Text
	if text == start && bot.OnPeerStart != "" {
		e := s.reply(api, upd, bot.OnPeerStart)
		if e != nil {
			return fmt.Errorf("s.reply: %w", e)
		}
		return nil
	}

	var match bool
kws:
	for _, kw := range bot.Keywords {
		for _, in := range kw.In {
			if strings.Contains(text, in) {
				if kw.Ban {
					e := s.peerRepo.CreateMuted(ctx, bot.ID, upd.Message.From.ID, upd.Message.Chat.ID)
					if e != nil {
						return fmt.Errorf("s.peerRepo.CreateMuted: %w", e)
					}

					e = s.reply(api, upd, kw.Out)
					if e != nil {
						return fmt.Errorf("s.reply: %w", e)
					}
					return nil
				}

				e := s.peerRepo.Create(ctx, bot.ID, upd.Message.From.ID, upd.Message.Chat.ID)
				if e != nil {
					return fmt.Errorf("s.peerRepo.Create: %w", e)
				}

				e = s.reply(api, upd, kw.Out)
				if e != nil {
					return fmt.Errorf("s.reply: %w", e)
				}

				if bot.OwnerUserChatID != 0 {
					_, e = api.Send(tgbotapi.MessageConfig{
						BaseChat: tgbotapi.BaseChat{
							ChatID: bot.OwnerUserChatID,
						},
						Text: fmt.Sprintf(`%s%d
%s%d
%s%d

%s / %s
%s

Бот ответил
%s

Вы можете 'Ответить' на это сообщение текстом, чтобы ответить отправителю, или 'Ответить' '%s' чтобы забанить его`,
							userForward, upd.Message.From.ID,
							chatForward, upd.Message.Chat.ID,
							messageForward, upd.Message.MessageID,
							tplName(upd.Message.From.FirstName), tplUsername(upd.Message.From.Username),
							text,
							kw.Out,
							mute),
					})
					if e != nil {
						return fmt.Errorf("api.Send: %w", e)
					}
				}

				match = true
				break kws
			}
		}
	}

	if !match {
		e := s.peerRepo.Create(ctx, bot.ID, upd.Message.From.ID, upd.Message.Chat.ID)
		if e != nil {
			return fmt.Errorf("s.peerRepo.Create: %w", e)
		}

		_, e = api.Send(tgbotapi.MessageConfig{
			BaseChat: tgbotapi.BaseChat{
				ChatID: bot.OwnerUserChatID,
			},
			Text: fmt.Sprintf(`%s%d
%s%d
%s%d

%s / %s
%s

Бот не ответил

Вы можете 'Ответить' на это сообщение текстом, чтобы ответить отправителю, или 'Ответить' %s чтобы забанить его`,
				userForward, upd.Message.From.ID,
				chatForward, upd.Message.Chat.ID,
				messageForward, upd.Message.MessageID,
				tplName(upd.Message.From.FirstName), tplUsername(upd.Message.From.Username),
				text,
				mute),
		})
		if e != nil {
			return fmt.Errorf("api.Send: %w", e)
		}
	}
	return nil
}

func (s *service) reply(api *tgbotapi.BotAPI, upd update, text string) error {
	_, err := api.Send(tgbotapi.MessageConfig{
		BaseChat: tgbotapi.BaseChat{
			ChatID:           upd.Message.Chat.ID,
			ReplyToMessageID: int(upd.Message.MessageID),
		},
		Text: text,
	})
	if err != nil {
		return fmt.Errorf("api.Send: %w", err)
	}
	return nil
}

func (s *service) replyOK(api *tgbotapi.BotAPI, upd update, text string) error {
	err := s.reply(api, upd, "OK. "+text)
	if err != nil {
		return fmt.Errorf("s.reply: %w", err)
	}
	return nil
}

func (s *service) replyErr(api *tgbotapi.BotAPI, upd update, text string) error {
	err := s.reply(api, upd, fmt.Sprintf(`Ошибка. %s
%s`, text, help))
	if err != nil {
		return fmt.Errorf("s.reply: %w", err)
	}
	return nil
}

func boolToRU(b bool) string {
	if b {
		return yes
	}
	return no
}

func ruToBool(s string) (bool, bool) {
	low := strings.ToLower(s)
	switch low {
	case yes:
		return true, true
	case no:
		return false, true
	default:
		return false, false
	}
}

func (s *service) parseKeywordsAndMode(in string) ([]Keyword, mode, bool) {
	words := strings.Split(in, delim)
	if len(words)-1%3 != 0 || len(words) < 4 {
		return nil, 0, false
	}

	modeInt, err := strconv.Atoi(words[0])
	if err != nil {
		return nil, 0, false
	}

	var m mode
	switch mode(modeInt) {
	case OnlyFirst, Always:
		m = mode(modeInt)
	default:
		return nil, 0, false
	}

	var keywords []Keyword
	for i := 1; i < len(words); i += 3 {
		out := words[i+1]
		if utf8.RuneCountInString(out) > int(s.outLimitChars) {
			return nil, 0, false
		}

		rawInKws := strings.Split(words[i], comma)
		if len(rawInKws) > int(s.inLimitPerKeyword) {
			return nil, 0, false
		}

		unique := map[string]struct{}{}
		for _, kw := range rawInKws {
			k := strings.TrimSpace(kw)
			if k == "" || utf8.RuneCountInString(k) > int(s.inLimitChars) {
				return nil, 0, false
			}

			unique[k] = struct{}{}
		}

		kwIn := make([]string, len(unique))
		ix := 0
		for kw := range unique {
			kwIn[ix] = kw
			ix += 1
		}

		ban, ok := ruToBool(words[i+2])
		if !ok {
			return nil, 0, false
		}

		keywords = append(keywords, Keyword{
			In:  kwIn,
			Out: out,
			Ban: ban,
		})
	}
	if len(keywords) > int(s.keywordsLimit) {
		return nil, 0, false
	}

	return keywords, m, true
}

func tplName(in string) string {
	name := "Нет имени"
	if in != "" {
		name = in
	}
	return name
}

func tplUsername(in string) string {
	username := "Нет username"
	if in != "" {
		username = "@" + in
	}
	return username
}
