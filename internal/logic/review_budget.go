package logic

import (
	"CR-Agent/internal/model"
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
)

const (
	defaultReviewBudgetYuan  = 10.0
	defaultInputPrice        = 2.1
	defaultOutputPrice       = 8.4
	inputFramingTokenReserve = 1024
)

type reviewBudgetContextKey struct{}

type reviewBudgetReservation struct {
	reservedMicros int64
	outputTokens   int
}

type reviewBudgetMeter struct {
	mu             sync.Mutex
	job            *model.ReviewJob
	limitMicros    int64
	reservedMicros int64
	spentMicros    int64
	inputPrice     float64
	outputPrice    float64
	modelName      string
	checkpoint     func() error
}

func newReviewBudgetMeter(job *model.ReviewJob, cfg Config, checkpoint func() error) *reviewBudgetMeter {
	limitMicros := job.BudgetMicros
	if limitMicros <= 0 {
		budgetYuan := job.BudgetYuan
		if budgetYuan <= 0 {
			budgetYuan = cfg.ReviewBudgetYuan
		}
		if budgetYuan <= 0 {
			budgetYuan = defaultReviewBudgetYuan
		}
		limitMicros = model.YuanToMicros(budgetYuan)
		job.BudgetMicros = limitMicros
		job.BudgetYuan = model.MicrosToYuan(limitMicros)
	} else {
		job.BudgetYuan = model.MicrosToYuan(limitMicros)
	}
	if job.SpentMicros <= 0 && job.SpentYuan > 0 {
		job.SpentMicros = model.YuanToMicros(job.SpentYuan)
	}
	job.SpentYuan = model.MicrosToYuan(job.SpentMicros)
	if cfg.InputPriceYuanPerMillion <= 0 {
		cfg.InputPriceYuanPerMillion = defaultInputPrice
	}
	if cfg.OutputPriceYuanPerMillion <= 0 {
		cfg.OutputPriceYuanPerMillion = defaultOutputPrice
	}
	modelName := cfg.DeepSeekModel
	if strings.TrimSpace(modelName) == "" {
		modelName = "deepseek-flash"
	}
	return &reviewBudgetMeter{
		job:         job,
		limitMicros: limitMicros,
		spentMicros: job.SpentMicros,
		inputPrice:  cfg.InputPriceYuanPerMillion,
		outputPrice: cfg.OutputPriceYuanPerMillion,
		modelName:   modelName,
		checkpoint:  checkpoint,
	}
}

func withReviewBudget(ctx context.Context, meter *reviewBudgetMeter) context.Context {
	return context.WithValue(ctx, reviewBudgetContextKey{}, meter)
}

func reviewBudgetFrom(ctx context.Context) *reviewBudgetMeter {
	meter, _ := ctx.Value(reviewBudgetContextKey{}).(*reviewBudgetMeter)
	return meter
}

func (m *reviewBudgetMeter) reserve(estimatedInputTokens, requestedOutputTokens int) (reviewBudgetReservation, error) {
	if m == nil {
		return reviewBudgetReservation{outputTokens: requestedOutputTokens}, nil
	}
	if estimatedInputTokens < inputFramingTokenReserve {
		estimatedInputTokens = inputFramingTokenReserve
	}
	if requestedOutputTokens <= 0 {
		return reviewBudgetReservation{}, fmt.Errorf("模型输出 token 上限无效")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	remaining := m.limitMicros - m.spentMicros - m.reservedMicros
	inputCost := tokenCostMicros(estimatedInputTokens, m.inputPrice)
	if inputCost >= remaining {
		return reviewBudgetReservation{}, fmt.Errorf("审查预算不足，无法继续发送模型请求")
	}
	outputAllowance := int(math.Floor(float64(remaining-inputCost) / m.outputPrice))
	if outputAllowance < 1 {
		return reviewBudgetReservation{}, fmt.Errorf("审查预算不足，无法继续生成模型输出")
	}
	if outputAllowance > requestedOutputTokens {
		outputAllowance = requestedOutputTokens
	}
	reserved := inputCost + tokenCostMicros(outputAllowance, m.outputPrice)
	m.reservedMicros += reserved
	return reviewBudgetReservation{
		reservedMicros: reserved,
		outputTokens:   outputAllowance,
	}, nil
}

func (m *reviewBudgetMeter) settle(reservation reviewBudgetReservation, inputTokens, outputTokens int, usageKnown bool) int64 {
	if m == nil {
		return 0
	}
	charge := reservation.reservedMicros
	if usageKnown {
		charge = tokenCostMicros(inputTokens, m.inputPrice) + tokenCostMicros(outputTokens, m.outputPrice)
	}
	m.mu.Lock()
	m.reservedMicros -= reservation.reservedMicros
	if m.reservedMicros < 0 {
		m.reservedMicros = 0
	}
	if charge > math.MaxInt64-m.spentMicros {
		m.spentMicros = math.MaxInt64
	} else {
		m.spentMicros += charge
	}
	m.job.SpentMicros = m.spentMicros
	m.job.SpentYuan = model.MicrosToYuan(m.spentMicros)
	m.mu.Unlock()
	return charge
}

func (m *reviewBudgetMeter) persist() error {
	if m == nil || m.checkpoint == nil {
		return nil
	}
	return m.checkpoint()
}

func (m *reviewBudgetMeter) isOverLimit() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.spentMicros > m.limitMicros
}

func (m *reviewBudgetMeter) tracePricing() (string, float64, float64) {
	if m == nil {
		return "", 0, 0
	}
	return m.modelName, m.inputPrice, m.outputPrice
}

func tokenCostMicros(tokens int, priceYuanPerMillion float64) int64 {
	if tokens <= 0 || priceYuanPerMillion <= 0 {
		return 0
	}
	cost := math.Ceil(float64(tokens) * priceYuanPerMillion)
	if cost > float64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(cost)
}
