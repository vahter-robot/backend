package parent_bot

import (
	"context"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog"
	"github.com/vahter-robot/backend/pkg/child_bot"
	"github.com/vahter-robot/backend/pkg/parent_state"
	"github.com/vahter-robot/backend/pkg/peer"
	"github.com/vahter-robot/backend/pkg/user"
	"go.mongodb.org/mongo-driver/bson/primitive"
	tb "gopkg.in/tucnak/telebot.v2"
	"net"
	"strings"
	"time"
)

type service struct {
	bot                   *tb.Bot
	parentStateRepo       *parent_state.Repo
	userRepo              *user.Repo
	peerRepo              *peer.Repo
	childBotRepo          *child_bot.Repo
	childBotHost          string
	childTokenPathPrefix  string
	childBotsLimitPerUser uint16
	logger                zerolog.Logger
}

const (
	start     = "/start"
	help      = "/help"
	createBot = "/new_bot"
	deleteBot = "/delete_bot"
	cleanup   = "/cleanup"
)

func NewService(
	logger zerolog.Logger,
	parentBotHost,
	parentBotPort,
	parentTokenPathPrefix,
	parentBotToken string,
	parentStateRepo *parent_state.Repo,
	userRepo *user.Repo,
	peerRepo *peer.Repo,
	childBotRepo *child_bot.Repo,
	childBotHost,
	childTokenPathPrefix string,
	childBotsLimitPerUser uint16,
) (
	*service,
	error,
) {
	publicURL := fmt.Sprintf("%s/%s/%s", parentBotHost, parentTokenPathPrefix, parentBotToken)
	poller := &tb.Webhook{
		Listen: net.JoinHostPort("0.0.0.0", parentBotPort),
		Endpoint: &tb.WebhookEndpoint{
			PublicURL: publicURL,
		},
	}

	b, err := tb.NewBot(tb.Settings{
		Token:  parentBotToken,
		Poller: poller,
	})
	if err != nil {
		return nil, fmt.Errorf("tb.NewBot: %w", err)
	}

	return &service{
		bot:                   b,
		parentStateRepo:       parentStateRepo,
		userRepo:              userRepo,
		peerRepo:              peerRepo,
		childBotRepo:          childBotRepo,
		childBotHost:          childBotHost,
		childTokenPathPrefix:  childTokenPathPrefix,
		childBotsLimitPerUser: childBotsLimitPerUser,
		logger:                logger.With().Str("package", "parent_bot").Logger(),
	}, nil
}

func (b *service) Serve(ctx context.Context) {
	b.initHandlers()
	go func() {
		<-ctx.Done()
		b.bot.Stop()
	}()
	b.bot.Start()
}

func (b *service) handleCreateBot(msg *tb.Message) {
	if !hasIDs(msg) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	usr, err := b.userRepo.Create(ctx, int64(msg.Sender.ID), msg.Chat.ID)
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	err = b.parentStateRepo.SetScene(ctx, usr.ID, parent_state.CreateBot)
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	b.reply(msg, fmt.Sprintf(
		"%s, давайте создадим вашего Вахтёр-бота. Перейдите в @BotFather, создайте бота (команда "+
			"'newbot') и отправьте в чат токен бота. Токен выглядит примерно так: "+
			"'123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11'",
		msg.Sender.FirstName,
	))
}

func (b *service) handleHelp(msg *tb.Message) {
	if !hasIDs(msg) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	usr, err := b.userRepo.Create(ctx, int64(msg.Sender.ID), msg.Chat.ID)
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	err = b.parentStateRepo.SetScene(ctx, usr.ID, parent_state.None)
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	b.reply(msg, fmt.Sprintf(`Команды

%s — создать нового бота
%s — вывести список ботов и удалить выбранного
%s — выйти из любого меню и показать это сообщение

Для настройки конкретного бота, используйте чат с ним`, createBot, deleteBot, help))
}

func (b *service) handleDeleteBot(msg *tb.Message) {
	if !hasIDs(msg) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	usr, err := b.userRepo.Create(ctx, int64(msg.Sender.ID), msg.Chat.ID)
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	err = b.parentStateRepo.SetScene(ctx, usr.ID, parent_state.DeleteBot)
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	bots, err := b.childBotRepo.GetByUserID(ctx, usr.ID)
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	text := fmt.Sprintf(
		"Список ваших ботов (%d/%d). Кликните на ID чтобы удалить бота. Нажмите %s чтобы выйти в меню",
		len(bots),
		b.childBotsLimitPerUser,
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

ID /%s
@%s`, b2.ID.Hex(), api.Self.UserName)
	}

	b.replyOK(msg, text)
}

func (b *service) handleCleanup(msg *tb.Message) {
	if !hasIDs(msg) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	usr, err := b.userRepo.Create(ctx, int64(msg.Sender.ID), msg.Chat.ID)
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	bots, err := b.childBotRepo.GetByUserID(ctx, usr.ID)
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	for _, b2 := range bots {
		_, e := tgbotapi.NewBotAPI(b2.Token)
		if e != nil {
			er := b.deleteChildBot(ctx, usr.ID, b2.ID)
			if er != nil {
				b.replyFatalErr(msg, er)
				return
			}
		}
	}

	b.replyOK(msg, fmt.Sprintf("Боты с некорректными токенами удалены. %s", help))
}

func (b *service) deleteChildBot(ctx context.Context, userID, childBotID primitive.ObjectID) error {
	err := b.childBotRepo.Delete(ctx, userID, childBotID)
	if err != nil {
		return fmt.Errorf("b.childBotRepo.Delete: %w", err)
	}

	err = b.peerRepo.DeleteByChildBotID(ctx, childBotID)
	if err != nil {
		return fmt.Errorf("b.peerRepo.DeleteByChildBotID: %w", err)
	}
	return nil
}

func (b *service) handleOnText(msg *tb.Message) {
	if !hasIDs(msg) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	usr, err := b.userRepo.Create(ctx, int64(msg.Sender.ID), msg.Chat.ID)
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	scene, err := b.parentStateRepo.GetScene(ctx, usr.ID)
	if err != nil {
		b.replyFatalErr(msg, err)
		return
	}

	switch scene {
	case parent_state.CreateBot:
		const invalidBotToken = "Некорректный токен бота"

		token := msg.Text
		if token == "" {
			b.replyErr(msg, invalidBotToken)
			return
		}

		count, e := b.childBotRepo.CountByUserID(ctx, usr.ID)
		if e != nil {
			b.replyFatalErr(msg, e)
			return
		}

		if count >= int64(b.childBotsLimitPerUser) {
			b.replyErr(msg, fmt.Sprintf("Максимальное количество ботов — %d, чтобы добавить нового "+
				"удалите одного из текущих %s", b.childBotsLimitPerUser, deleteBot))
			return
		}

		api, e := tgbotapi.NewBotAPI(token)
		if e != nil {
			b.replyErr(msg, invalidBotToken)
			return
		}

		e = b.childBotRepo.Create(ctx, usr.ID, token)
		if e != nil {
			b.replyFatalErr(msg, e)
			return
		}

		_, e = api.SetWebhook(tgbotapi.NewWebhook(fmt.Sprintf(
			"%s/%s/%s", b.childBotHost, b.childTokenPathPrefix, token,
		)))
		if e != nil {
			b.replyFatalErr(msg, e)
			return
		}

		e = b.parentStateRepo.SetScene(ctx, usr.ID, parent_state.None)
		if e != nil {
			b.replyFatalErr(msg, e)
			return
		}

		b.replyOK(msg, "Бот создан. Настройте его в чате с @"+api.Self.UserName)
	case parent_state.DeleteBot:
		botID, e := primitive.ObjectIDFromHex(strings.Replace(msg.Text, "/", "", 1))
		if e != nil {
			b.replyErr(msg, "Некорректный ID бота")
			return
		}

		e = b.deleteChildBot(ctx, usr.ID, botID)
		if e != nil {
			b.replyFatalErr(msg, e)
			return
		}

		b.replyOK(msg, withHelp("Бот удален"))
	default:
		b.replyErr(msg, "Неизвестная команда")
		return
	}
}

func (b *service) initHandlers() {
	b.bot.Handle(start, b.handleCreateBot)
	b.bot.Handle(help, b.handleHelp)
	b.bot.Handle(createBot, b.handleCreateBot)
	b.bot.Handle(deleteBot, b.handleDeleteBot)
	b.bot.Handle(cleanup, b.handleCleanup)
	b.bot.Handle(tb.OnText, b.handleOnText)
}

func (b *service) reply(msg *tb.Message, text string) bool {
	if msg == nil {
		return false
	}

	_, err := b.bot.Send(msg.Sender, text, &tb.SendOptions{
		ReplyTo: msg,
	})
	if err != nil {
		b.logger.Error().Err(err).Send()
		return false
	}
	return true
}

func (b *service) replyOK(msg *tb.Message, str string) bool {
	return b.reply(msg, "OK. "+str)
}

func (b *service) replyErr(msg *tb.Message, str string) bool {
	return b.reply(msg, fmt.Sprintf(`Ошибка. %s
%s`, str, help))
}

func (b *service) replyFatalErr(msg *tb.Message, err error) bool {
	b.logger.Error().Err(err).Send()
	return b.reply(msg, fmt.Sprintf(
		`Произошла ошибка, мы уже знаем о ней и работаем над исправлением
%s`,
		help,
	))
}

func hasIDs(msg *tb.Message) bool {
	return msg != nil && msg.Sender != nil && msg.Sender.ID != 0 && msg.Chat != nil && msg.Chat.ID != 0
}

func withHelp(str string) string {
	return str + "\n" + help
}
