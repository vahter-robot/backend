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
	"github.com/vahter-robot/backend/pkg/reply"
	"github.com/vahter-robot/backend/pkg/user"
	"go.mongodb.org/mongo-driver/bson/primitive"
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
	replyRepo            *reply.Repo
	keywordsLimitPerBot  uint16
	inLimitPerKeyword    uint16
	inLimitChars         uint16
	outLimitChars        uint16
	parentBotUsername    string
	setWebhooks          bool
	timeoutOnHandle      bool
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
	replyRepo *reply.Repo,
	keywordsLimitPerBot,
	inLimitPerKeyword,
	inLimitChars,
	outLimitChars uint16,
	parentBotUsername string,
	setWebhooks,
	timeoutOnHandle bool,
) *service {
	return &service{
		childBotHost:         childBotHost,
		childBotPort:         childBotPort,
		childTokenPathPrefix: childTokenPathPrefix,
		childStateRepo:       childStateRepo,
		userRepo:             userRepo,
		peerRepo:             peerRepo,
		childBotRepo:         childBotRepo,
		replyRepo:            replyRepo,
		keywordsLimitPerBot:  keywordsLimitPerBot,
		inLimitPerKeyword:    inLimitPerKeyword,
		inLimitChars:         inLimitChars,
		outLimitChars:        outLimitChars,
		parentBotUsername:    parentBotUsername,
		setWebhooks:          setWebhooks,
		timeoutOnHandle:      timeoutOnHandle,
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

	messageForward = "✉️ "
	mute           = "mute"
	unmute         = "unmute"

	yes   = "да"
	no    = "нет"
	delim = "\n===\n"
	comma = ","
)

func (s *service) Serve(ctx context.Context) error {
	if s.setWebhooks {
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
		rc := context.Background()
		if s.timeoutOnHandle {
			c, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			rc = c
		}

		token := strings.TrimPrefix(r.URL.Path, pathPrefix)

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
	if strings.HasPrefix(replyText, messageForward) && upd.Message.ReplyToMessage.From.Username == api.Self.UserName {
		t := strings.Split(replyText, "\n")
		if len(t) >= 1 {
			id, er := primitive.ObjectIDFromHex(strings.TrimPrefix(t[0], messageForward))
			if er != nil {
				return fmt.Errorf("primitive.ObjectIDFromHex: %w", er)
			}

			repl, er := s.replyRepo.GetByID(ctx, id)
			if er != nil {
				return fmt.Errorf("s.replyRepo.GetByID: %w", er)
			}

			switch text {
			case mute:
				e := s.peerRepo.CreateMuted(ctx, bot.ID, repl.TgUserID, repl.TgChatID)
				if e != nil {
					return fmt.Errorf("s.peerRepo.CreateMuted: %w", e)
				}

				e = s.replyOK(api, upd, "Заблокирован")
				if e != nil {
					return fmt.Errorf("s.replyOK: %w", e)
				}
				return nil
			case unmute:
				e := s.peerRepo.CreateUnMuted(ctx, bot.ID, repl.TgUserID, repl.TgChatID)
				if e != nil {
					return fmt.Errorf("s.peerRepo.CreateUnMuted: %w", e)
				}

				e = s.replyOK(api, upd, "Разблокирован")
				if e != nil {
					return fmt.Errorf("s.replyOK: %w", e)
				}
				return nil
			}

			_, er = api.Send(tgbotapi.MessageConfig{
				BaseChat: tgbotapi.BaseChat{
					ChatID:           repl.TgChatID,
					ReplyToMessageID: int(repl.TgMessageID),
				},
				Text: text,
			})
			if er != nil {
				er2 := s.replyErr(api, upd, "Не отправлено. Возможно пользователь остановил бота")
				if er2 != nil {
					return fmt.Errorf("s.replyErr: %w", er2)
				}
				return nil
			}

			er = s.replyOK(api, upd, "Отправлено")
			if er != nil {
				return fmt.Errorf("s.replyOK: %w", er)
			}
			return nil
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
- Далее напишите нужно ли банить отправителя, если данный фильтр сработал на его сообщение. Если указано 'да' – бот ответит отправителю, далее бот игнорирует любые сообщения от него, бот не пересылает вам ни первое ни последующие сообщения от данного пользователя. Если указано 'нет' — бот ответит отправителю, перешлет вам исходное сообщение и ответ на него, вы сможете вести переписку с отправителем анонимно через бота, а забанить ответив '%s', разбанить '%s';
- Все элементы с новой строки и разделены '==='.

Например:

2
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
нет`, mute, unmute))
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
	keywords := make([]string, len(bot.Keywords))
	for i, word := range bot.Keywords {
		keywords[i] = fmt.Sprintf(`%s
===
%s
===
%s`, strings.Join(word.In, comma), word.Out, boolToRU(word.Ban))
	}

	err := s.reply(api, upd, fmt.Sprintf(`Ключевые слова (%d/%d). Формат:
Режим работы. '1' — реагирует только на первое сообщение. '2' — реагирует на все
===
Ключевое слово
===
Автоответ
===
Банить

%d
===
%s

%s`, len(bot.Keywords), s.keywordsLimitPerBot, bot.Mode, strings.Join(keywords, delim), help))
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
	if bot.Mode == None {
		return nil
	}

	peerUser, peerFound, err := s.peerRepo.Get(ctx, bot.ID, upd.Message.From.ID)
	if err != nil {
		return fmt.Errorf("s.peerRepo.Create: %w", err)
	}
	if peerUser.Muted {
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

	if !peerFound {
		e := s.peerRepo.Create(ctx, bot.ID, upd.Message.From.ID, upd.Message.Chat.ID)
		if e != nil {
			return fmt.Errorf("s.peerRepo.Create: %w", e)
		}
	}

	if bot.Mode == OnlyFirst && peerFound {
		id, e := s.replyRepo.Create(ctx, bot.ID, upd.Message.From.ID, upd.Message.Chat.ID, upd.Message.MessageID)
		if e != nil {
			return fmt.Errorf("s.replyRepo.Create: %w", e)
		}

		_, e = api.Send(tgbotapi.MessageConfig{
			BaseChat: tgbotapi.BaseChat{
				ChatID: bot.OwnerUserChatID,
			},
			Text: fmt.Sprintf(`%s%s
%s / %s:
%s

'Ответить' на это сообщение текстом, чтобы ответить отправителю, или '%s' чтобы забанить его, '%s' разбанить`,
				messageForward, id.Hex(),
				tplUsername(upd.Message.From.Username), tplName(upd.Message.From.FirstName),
				text,
				mute,
				unmute),
		})
		if e != nil {
			return fmt.Errorf("api.Send: %w", e)
		}
		return nil
	}

	lowText := strings.ToLower(text)

	var match bool
kws:
	for _, kw := range bot.Keywords {
		for _, in := range kw.In {
			if strings.Contains(lowText, in) {
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

				e := s.reply(api, upd, kw.Out)
				if e != nil {
					return fmt.Errorf("s.reply: %w", e)
				}

				if bot.OwnerUserChatID != 0 {
					id, er := s.replyRepo.Create(
						ctx,
						bot.ID,
						upd.Message.From.ID,
						upd.Message.Chat.ID,
						upd.Message.MessageID,
					)
					if er != nil {
						return fmt.Errorf("s.replyRepo.Create: %w", er)
					}

					_, er = api.Send(tgbotapi.MessageConfig{
						BaseChat: tgbotapi.BaseChat{
							ChatID: bot.OwnerUserChatID,
						},
						Text: fmt.Sprintf(`%s%s
%s / %s:
%s

Бот ответил:
%s

'Ответить' на это сообщение текстом, чтобы ответить отправителю, или '%s' чтобы забанить его, '%s' разбанить`,
							messageForward, id.Hex(),
							tplUsername(upd.Message.From.Username), tplName(upd.Message.From.FirstName),
							text,
							kw.Out,
							mute,
							unmute),
					})
					if er != nil {
						return fmt.Errorf("api.Send: %w", er)
					}
				}

				match = true
				break kws
			}
		}
	}

	if !match {
		id, e := s.replyRepo.Create(ctx, bot.ID, upd.Message.From.ID, upd.Message.Chat.ID, upd.Message.MessageID)
		if e != nil {
			return fmt.Errorf("s.replyRepo.Create: %w", e)
		}

		_, e = api.Send(tgbotapi.MessageConfig{
			BaseChat: tgbotapi.BaseChat{
				ChatID: bot.OwnerUserChatID,
			},
			Text: fmt.Sprintf(`%s%s
%s / %s:
%s

'Ответить' на это сообщение текстом, чтобы ответить отправителю, или '%s' чтобы забанить его, '%s' разбанить`,
				messageForward, id.Hex(),
				tplUsername(upd.Message.From.Username), tplName(upd.Message.From.FirstName),
				text,
				mute,
				unmute),
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
	if (len(words)-1)%3 != 0 || len(words) < 4 {
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
			k := strings.ToLower(strings.TrimSpace(kw))
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
	if len(keywords) > int(s.keywordsLimitPerBot) {
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
