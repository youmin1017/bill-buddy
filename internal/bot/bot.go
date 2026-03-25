package bot

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"bill-buddy/ent"
	"bill-buddy/ent/expense"
	"bill-buddy/internal/config"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/samber/do/v2"
)

// App wraps the bot and owns the Start lifecycle.
type App struct {
	b *tgbot.Bot
}

var Package = do.Package(
	do.Lazy(newBot),
	do.Lazy(newApp),
)

type handler struct {
	db *ent.Client
}

func newBot(i do.Injector) (*tgbot.Bot, error) {
	cfg := do.MustInvoke[*config.Config](i)
	db := do.MustInvoke[*ent.Client](i)

	h := &handler{db: db}

	b, err := tgbot.New(cfg.TelegramBotToken)
	if err != nil {
		return nil, err
	}

	b.RegisterHandler(tgbot.HandlerTypeMessageText, "/help", tgbot.MatchTypeExact, normalizeCommand(h.helpHandler))
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "/add", tgbot.MatchTypePrefix, normalizeCommand(h.addHandler))
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "/pay", tgbot.MatchTypePrefix, normalizeCommand(h.payHandler))
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "/list", tgbot.MatchTypeExact, normalizeCommand(h.listHandler))
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "/settle", tgbot.MatchTypeExact, normalizeCommand(h.settleHandler))
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "/delete", tgbot.MatchTypePrefix, normalizeCommand(h.deleteHandler))
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "/modify", tgbot.MatchTypePrefix, normalizeCommand(h.modifyHandler))

	return b, nil
}

func newApp(i do.Injector) (*App, error) {
	b := do.MustInvoke[*tgbot.Bot](i)
	app := &App{b: b}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	_ = cancel

	// Register commands for Telegram autocomplete
	b.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{
		Commands: []models.BotCommand{
			{Command: "add", Description: "新增一筆由你支付的分攤費用"},
			{Command: "pay", Description: "代付對方的全額費用"},
			{Command: "list", Description: "查詢歷史紀錄與結算"},
			{Command: "settle", Description: "結清所有帳目並清空紀錄"},
			{Command: "delete", Description: "刪除指定紀錄"},
			{Command: "modify", Description: "修改指定紀錄"},
			{Command: "help", Description: "顯示指令說明"},
		},
	})

	go func() {
		app.b.Start(ctx)
	}()

	return app, nil
}

// normalizeCommand strips the @botname suffix from commands so that
// "/settle@bill_buddy" is treated the same as "/settle".
func normalizeCommand(next tgbot.HandlerFunc) tgbot.HandlerFunc {
	return func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
		if update.Message != nil {
			if fields := strings.Fields(update.Message.Text); len(fields) > 0 {
				if idx := strings.Index(fields[0], "@"); idx != -1 {
					fields[0] = fields[0][:idx]
					update.Message.Text = strings.Join(fields, " ")
				}
			}
		}
		next(ctx, b, update)
	}
}

func sendText(ctx context.Context, b *tgbot.Bot, chatID int64, text string) {
	b.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
	})
}

func senderName(update *models.Update) string {
	u := update.Message.From
	if u == nil {
		return "Unknown"
	}
	if u.LastName != "" {
		return u.FirstName + " " + u.LastName
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	return u.FirstName
}

// parseArgs strips the command prefix and returns trimmed arguments.
func parseArgs(text string) []string {
	parts := strings.Fields(text)
	if len(parts) <= 1 {
		return nil
	}
	return parts[1:]
}

// /add <amount> [description] — shared expense, other person owes half
func (h *handler) addHandler(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	chatID := update.Message.Chat.ID
	args := parseArgs(update.Message.Text)
	if len(args) == 0 {
		sendText(ctx, b, chatID, "用法：/add <金額> [說明]\n例：/add 150 午餐")
		return
	}

	amount, err := strconv.ParseFloat(args[0], 64)
	if err != nil || amount <= 0 {
		sendText(ctx, b, chatID, "金額格式錯誤，請輸入正數。")
		return
	}

	desc := ""
	if len(args) > 1 {
		desc = strings.Join(args[1:], " ")
	}

	paidBy := update.Message.From.ID
	paidByName := senderName(update)

	exp, err := h.db.Expense.Create().
		SetChatID(chatID).
		SetAmount(amount).
		SetDescription(desc).
		SetPaidBy(paidBy).
		SetPaidByName(paidByName).
		SetForOther(false).
		Save(ctx)
	if err != nil {
		sendText(ctx, b, chatID, "新增失敗，請稍後再試。")
		return
	}

	msg := fmt.Sprintf("✅ 已新增 #%d\n💰 金額：$%.2f\n👤 支付者：%s", exp.ID, amount, paidByName)
	if desc != "" {
		msg += "\n📝 說明：" + desc
	}
	msg += fmt.Sprintf("\n（另一人待還 $%.2f）", amount/2)
	sendText(ctx, b, chatID, msg)
}

// /pay <amount> [description] — payer paid the full amount on behalf of the other person
func (h *handler) payHandler(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	chatID := update.Message.Chat.ID
	args := parseArgs(update.Message.Text)
	if len(args) == 0 {
		sendText(ctx, b, chatID, "用法：/pay <金額> [說明]\n例：/pay 200 代付房租")
		return
	}

	amount, err := strconv.ParseFloat(args[0], 64)
	if err != nil || amount <= 0 {
		sendText(ctx, b, chatID, "金額格式錯誤，請輸入正數。")
		return
	}

	desc := ""
	if len(args) > 1 {
		desc = strings.Join(args[1:], " ")
	}

	paidBy := update.Message.From.ID
	paidByName := senderName(update)

	exp, err := h.db.Expense.Create().
		SetChatID(chatID).
		SetAmount(amount).
		SetDescription(desc).
		SetPaidBy(paidBy).
		SetPaidByName(paidByName).
		SetForOther(true).
		Save(ctx)
	if err != nil {
		sendText(ctx, b, chatID, "新增失敗，請稍後再試。")
		return
	}

	msg := fmt.Sprintf("✅ 已新增代付 #%d\n💰 金額：$%.2f\n👤 支付者：%s", exp.ID, amount, paidByName)
	if desc != "" {
		msg += "\n📝 說明：" + desc
	}
	msg += fmt.Sprintf("\n（另一人待還全額 $%.2f）", amount)
	sendText(ctx, b, chatID, msg)
}

// /list — show all records and balance
func (h *handler) listHandler(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	chatID := update.Message.Chat.ID

	expenses, err := h.db.Expense.Query().
		Where(expense.ChatID(chatID)).
		Order(ent.Asc(expense.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		sendText(ctx, b, chatID, "查詢失敗，請稍後再試。")
		return
	}

	if len(expenses) == 0 {
		sendText(ctx, b, chatID, "📭 目前沒有任何紀錄。")
		return
	}

	// Build list
	var sb strings.Builder
	sb.WriteString("📋 歷史紀錄：\n\n")

	// net[uid] = total amount the other person owes this user
	net := make(map[int64]float64)
	names := make(map[int64]string)

	for _, exp := range expenses {
		names[exp.PaidBy] = exp.PaidByName

		var line string
		if exp.ForOther {
			line = fmt.Sprintf("#%d %s 代付 $%.2f", exp.ID, exp.PaidByName, exp.Amount)
			net[exp.PaidBy] += exp.Amount
		} else {
			line = fmt.Sprintf("#%d %s 支付 $%.2f", exp.ID, exp.PaidByName, exp.Amount)
			net[exp.PaidBy] += exp.Amount / 2
		}
		if exp.Description != "" {
			line += "（" + exp.Description + "）"
		}
		sb.WriteString(line + "\n")
	}

	// Calculate balance
	sb.WriteString("\n💳 結算：\n")
	if len(net) < 2 {
		for uid, n := range net {
			sb.WriteString(fmt.Sprintf("%s 待收 $%.2f\n", names[uid], n))
		}
	} else {
		var uids []int64
		for uid := range net {
			uids = append(uids, uid)
		}
		diff := net[uids[0]] - net[uids[1]]
		if math.Abs(diff) < 0.01 {
			sb.WriteString("✅ 已平衡，無需還款。\n")
		} else if diff > 0 {
			sb.WriteString(fmt.Sprintf("💸 %s 應還 %s $%.2f\n", names[uids[1]], names[uids[0]], diff))
		} else {
			sb.WriteString(fmt.Sprintf("💸 %s 應還 %s $%.2f\n", names[uids[0]], names[uids[1]], -diff))
		}
	}

	sendText(ctx, b, chatID, sb.String())
}

// /settle — show final balance, clear all records
func (h *handler) settleHandler(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	chatID := update.Message.Chat.ID

	expenses, err := h.db.Expense.Query().
		Where(expense.ChatID(chatID)).
		All(ctx)
	if err != nil {
		sendText(ctx, b, chatID, "查詢失敗，請稍後再試。")
		return
	}
	if len(expenses) == 0 {
		sendText(ctx, b, chatID, "📭 目前沒有任何紀錄，無需結清。")
		return
	}

	// Calculate final balance
	net := make(map[int64]float64)
	names := make(map[int64]string)
	for _, exp := range expenses {
		names[exp.PaidBy] = exp.PaidByName
		if exp.ForOther {
			net[exp.PaidBy] += exp.Amount
		} else {
			net[exp.PaidBy] += exp.Amount / 2
		}
	}

	var summary strings.Builder
	summary.WriteString("🤝 結清帳目\n\n")
	if len(net) < 2 {
		for uid, n := range net {
			summary.WriteString(fmt.Sprintf("%s 待收 $%.2f\n", names[uid], n))
		}
	} else {
		var uids []int64
		for uid := range net {
			uids = append(uids, uid)
		}
		diff := net[uids[0]] - net[uids[1]]
		if math.Abs(diff) < 0.01 {
			summary.WriteString("✅ 雙方已平衡，無需還款。\n")
		} else if diff > 0 {
			summary.WriteString(fmt.Sprintf("💸 %s 還給 %s $%.2f\n", names[uids[1]], names[uids[0]], diff))
		} else {
			summary.WriteString(fmt.Sprintf("💸 %s 還給 %s $%.2f\n", names[uids[0]], names[uids[1]], -diff))
		}
	}

	// Delete all records for this chat
	_, err = h.db.Expense.Delete().
		Where(expense.ChatID(chatID)).
		Exec(ctx)
	if err != nil {
		sendText(ctx, b, chatID, "結清失敗，請稍後再試。")
		return
	}

	summary.WriteString("\n🗑️ 所有紀錄已清空，重新開始。")
	sendText(ctx, b, chatID, summary.String())
}

// /delete <id>
func (h *handler) deleteHandler(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	chatID := update.Message.Chat.ID
	args := parseArgs(update.Message.Text)
	if len(args) == 0 {
		sendText(ctx, b, chatID, "用法：/delete <id>\n例：/delete 3")
		return
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		sendText(ctx, b, chatID, "請輸入有效的紀錄 ID。")
		return
	}

	// Verify the expense belongs to this chat
	exp, err := h.db.Expense.Get(ctx, id)
	if err != nil {
		sendText(ctx, b, chatID, fmt.Sprintf("找不到 #%d 的紀錄。", id))
		return
	}
	if exp.ChatID != chatID {
		sendText(ctx, b, chatID, "無法刪除其他群組的紀錄。")
		return
	}

	if err := h.db.Expense.DeleteOneID(id).Exec(ctx); err != nil {
		sendText(ctx, b, chatID, "刪除失敗，請稍後再試。")
		return
	}

	sendText(ctx, b, chatID, fmt.Sprintf("🗑️ 已刪除紀錄 #%d。", id))
}

// /modify <id> <amount> [description]
func (h *handler) modifyHandler(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	chatID := update.Message.Chat.ID
	args := parseArgs(update.Message.Text)
	if len(args) < 2 {
		sendText(ctx, b, chatID, "用法：/modify <id> <金額> [說明]\n例：/modify 3 180 午餐")
		return
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		sendText(ctx, b, chatID, "請輸入有效的紀錄 ID。")
		return
	}

	amount, err := strconv.ParseFloat(args[1], 64)
	if err != nil || amount <= 0 {
		sendText(ctx, b, chatID, "金額格式錯誤，請輸入正數。")
		return
	}

	desc := ""
	if len(args) > 2 {
		desc = strings.Join(args[2:], " ")
	}

	// Verify ownership
	exp, err := h.db.Expense.Get(ctx, id)
	if err != nil {
		sendText(ctx, b, chatID, fmt.Sprintf("找不到 #%d 的紀錄。", id))
		return
	}
	if exp.ChatID != chatID {
		sendText(ctx, b, chatID, "無法修改其他群組的紀錄。")
		return
	}

	updater := h.db.Expense.UpdateOneID(id).SetAmount(amount)
	if desc != "" {
		updater = updater.SetDescription(desc)
	}

	updated, err := updater.Save(ctx)
	if err != nil {
		sendText(ctx, b, chatID, "修改失敗，請稍後再試。")
		return
	}

	msg := fmt.Sprintf("✏️ 已修改 #%d\n💰 金額：$%.2f", updated.ID, updated.Amount)
	if updated.Description != "" {
		msg += "\n📝 說明：" + updated.Description
	}
	sendText(ctx, b, chatID, msg)
}

func (h *handler) helpHandler(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	msg := `📖 Bill Buddy 指令說明

/add <金額> [說明] - 新增一筆由你支付的分攤費用（另一人還一半）
/pay <金額> [說明] - 代付對方的費用（另一人還全額）
/list - 查詢歷史紀錄與結算
/settle - 結清所有帳目並清空紀錄
/delete <id> - 刪除指定紀錄
/modify <id> <金額> [說明] - 修改指定紀錄
/help - 顯示此說明`
	sendText(ctx, b, update.Message.Chat.ID, msg)
}
