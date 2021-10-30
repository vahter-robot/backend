package slave_bot

import (
	"context"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog"
	"github.com/vahter-robot/backend/pkg/master_state"
	"github.com/vahter-robot/backend/pkg/master_user"
	"github.com/vahter-robot/backend/pkg/slave_state"
	"go.mongodb.org/mongo-driver/bson/primitive"
	tb "gopkg.in/tucnak/telebot.v2"
	"strings"
	"time"
)

type bot struct {
	bot             *tb.Bot
	httpHost        string
	tokenPrefix     string
	masterStateRepo *master_state.Repo
	masterUserRepo  *master_user.Repo
	slaveBotRepo    *slave_bot.Repo
	slaveBotPath    string
	logger          zerolog.Logger
}

const (
	help        = "/help"
	setStart    = "/set_start"
	setKeywords = "/set_keywords"
)

func NewService(
	logger zerolog.Logger,
	httpHost,
	tokenPrefix,
	masterBotPath,
	masterHTTPPort,
	masterBotToken string,
	masterStateRepo *slave_state.Repo,
	masterUserRepo *slave.Repo,
	slaveBotRepo *slave_bot.Repo,
	slaveBotPath string,
) (
	*bot,
	error,
) {
	publicURL := fmt.Sprintf("%s/%s/%s/%s", httpHost, masterBotPath, tokenPrefix, masterBotToken)
	poller := &tb.Webhook{
		Listen: "0.0.0.0:" + masterHTTPPort,
		Endpoint: &tb.WebhookEndpoint{
			PublicURL: publicURL,
		},
	}

	b, err := tb.NewBot(tb.Settings{
		Token:  masterBotToken,
		Poller: poller,
	})
	if err != nil {
		return nil, fmt.Errorf("tb.NewBot: %w", err)
	}

	return &bot{
		bot:             b,
		httpHost:        httpHost,
		masterStateRepo: masterStateRepo,
		masterUserRepo:  masterUserRepo,
		slaveBotRepo:    slaveBotRepo,
		slaveBotPath:    slaveBotPath,
		logger:          logger.With().Str("package", "master_bot").Logger(),
	}, nil
}

func (b *bot) Serve(ctx context.Context) {
	b.initHandlers()
	go func() {
		<-ctx.Done()
		b.bot.Stop()
	}()
	b.logger.Debug().Msg("bot started")
	b.bot.Start()
	b.logger.Debug().Msg("bot stopped")
}

func (b *bot) createBotHandler(msg *tb.Message) {
	if !hasSenderID(msg) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	usr, err := b.masterUserRepo.Create(ctx, int64(msg.Sender.ID))
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	err = b.masterStateRepo.SetScene(ctx, usr.ID, master_state.CreateBot)
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	b.reply(msg, fmt.Sprintf(
		"%s, давайте создадим вашего Вахтёр-бота. Перейдите в @BotFather, создайте бота (команда "+
			"`newbot`) и отправьте сюда токен бота. Токен выглядит примерно так: "+
			"`123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11`",
		msg.Sender.FirstName,
	))
}

func (b *bot) helpHandler(msg *tb.Message) {
	if !hasSenderID(msg) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	usr, err := b.masterUserRepo.Create(ctx, int64(msg.Sender.ID))
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	err = b.masterStateRepo.SetScene(ctx, usr.ID, master_state.None)
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	b.reply(msg, fmt.Sprintf(`*Команды*

%s — создать нового бота
%s — вывести список ботов и удалить выбранные
%s — выйти из любого меню и показать это сообщение

Для настройки конкретного бота, используйте чат с ним.`, setKeywords, deleteBot, help))
}

func (b *bot) deleteBotHandler(msg *tb.Message) {
	if !hasSenderID(msg) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	usr, err := b.masterUserRepo.Create(ctx, int64(msg.Sender.ID))
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	err = b.masterStateRepo.SetScene(ctx, usr.ID, master_state.DeleteBot)
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	bots, err := b.slaveBotRepo.Get(ctx, usr.ID)
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	text := fmt.Sprintf(
		"Список ваших ботов. Кликните на ID чтобы удалить бота. Нажмите %s чтобы выйти в меню",
		help,
	)
	for _, b2 := range bots {
		api, e := tgbotapi.NewBotAPI(b2.Token)
		if e != nil {
			b.replyErr(msg, fmt.Sprintf("Токен некорректный. Похоже что вы удалили одного из ботов через "+
				"@BotFather. Нажмите %s чтобы удалить ботов с неработающими токенами. Боты с "+
				"корректными токенами удалены не будут", cleanup))
			return
		}

		text += fmt.Sprintf(`

%s
%s`, api.Self.UserName, b2.ID.Hex())
	}

	b.replyOK(msg, text)
}

func (b *bot) cleanupHandler(msg *tb.Message) {
	if !hasSenderID(msg) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	usr, err := b.masterUserRepo.Create(ctx, int64(msg.Sender.ID))
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	bots, err := b.slaveBotRepo.Get(ctx, usr.ID)
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	for _, b2 := range bots {
		_, e := tgbotapi.NewBotAPI(b2.Token)
		if e != nil {
			er := b.deleteSlaveBot(ctx, usr.ID, b2.ID)
			if er != nil {
				b.replyFatalErr(msg, er)
				return
			}
		}
	}

	b.replyOK(msg, fmt.Sprintf("Боты с некорректными токенами удалены. %s", help))
}

func (b *bot) deleteSlaveBot(ctx context.Context, masterUserID, slaveBotID primitive.ObjectID) error {
	err := b.slaveBotRepo.Delete(ctx, masterUserID, slaveBotID)
	if err != nil {
		return fmt.Errorf("b.slaveBotRepo.Delete: %w", err)
	}
	return nil
}

func (b *bot) onTextHandler(msg *tb.Message) {
	if !hasSenderID(msg) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	usr, err := b.masterUserRepo.Create(ctx, int64(msg.Sender.ID))
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	scene, err := b.masterStateRepo.GetScene(ctx, usr.ID)
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	switch scene {
	case master_state.CreateBot:
		const invalidBotToken = "Некорректный токен бота"

		token := msg.Text
		if token == "" {
			b.replyErr(msg, invalidBotToken)
			return
		}

		count, e := b.slaveBotRepo.CountByMasterUserID(ctx, usr.ID)
		if e != nil {
			b.replyFatalErr(msg, e)
			return
		}

		if count >= maxBots {
			b.replyErr(msg, fmt.Sprintf("Максимальное количество ботов — %d, чтобы добавить нового "+
				"удалите одного из текущих %s", maxBots, deleteBot))
		}

		api, e := tgbotapi.NewBotAPI(token)
		if e != nil {
			b.replyErr(msg, invalidBotToken)
			return
		}

		e = b.slaveBotRepo.Create(ctx, usr.ID, token)
		if e != nil {
			b.replyFatalErr(msg, e)
			return
		}

		_, e = api.SetWebhook(tgbotapi.NewWebhook(fmt.Sprintf(
			"%s/%s/%s/%s", b.httpHost, b.slaveBotPath, b.tokenPrefix, token,
		)))
		if e != nil {
			b.replyFatalErr(msg, e)
			return
		}

		e = b.masterStateRepo.SetScene(ctx, usr.ID, master_state.None)
		if e != nil {
			b.replyFatalErr(msg, e)
			return
		}

		b.replyOK(msg, "Бот создан. Настройте его в чате с @"+api.Self.UserName)
	case master_state.DeleteBot:
		botID, e := primitive.ObjectIDFromHex(strings.Replace(msg.Text, "/", "", 1))
		if e != nil {
			b.replyErr(msg, "Некорректный ID бота")
			return
		}

		e = b.deleteSlaveBot(ctx, usr.ID, botID)
		if e != nil {
			b.replyFatalErr(msg, e)
			return
		}

		b.replyOK(msg, withHelp("Бот удален"))
	}
}

func (b *bot) initHandlers() {
	b.bot.Handle(setStart, b.createBotHandler)
	b.bot.Handle(help, b.helpHandler)
	b.bot.Handle(setKeywords, b.createBotHandler)
	b.bot.Handle(deleteBot, b.deleteBotHandler)
	b.bot.Handle(cleanup, b.cleanupHandler)
	b.bot.Handle(tb.OnText, b.onTextHandler)
}

func (b *bot) reply(msg *tb.Message, text string) bool {
	if msg == nil {
		return false
	}

	_, err := b.bot.Send(msg.Sender, text, &tb.SendOptions{
		ReplyTo: msg,
	})
	if err != nil {
		b.logger.Error().Err(err)
		return false
	}
	return true
}

func (b *bot) replyOK(msg *tb.Message, str string) bool {
	return b.reply(msg, "OK. "+str)
}

func (b *bot) replyErr(msg *tb.Message, str string) bool {
	return b.reply(msg, fmt.Sprintf(`Ошибка. %s
%s`, str, help))
}

func (b *bot) replyFatalErr(msg *tb.Message, err error) bool {
	b.logger.Error().Err(err).Send()
	return b.reply(msg, fmt.Sprintf(
		`Произошла ошибка, мы уже знаем о ней и работаем над исправлением
%s`,
		help,
	))
}

func hasSenderID(msg *tb.Message) bool {
	return msg != nil && msg.Sender != nil && msg.Sender.ID != 0
}

func withHelp(str string) string {
	return str + "\n" + help
}
