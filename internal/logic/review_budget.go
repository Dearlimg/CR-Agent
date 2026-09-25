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
	pricing        reviewModelPricing
}

type reviewModelPricing struct {
	modelName   string
	inputPrice  float64
	outputPrice float64
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
		job:            job,
		limitMicros:    limitMicros,
		reservedMicros: job.ReservedMicros,
		spentMicros:    job.SpentMicros,
		inputPrice:     cfg.InputPriceYuanPerMillion,
		outputPrice:    cfg.OutputPriceYuanPerMillion,
		modelName:      modelName,
		checkpoint:     checkpoint,
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
	return m.reserveForModel(estimatedInputTokens, requestedOutputTokens, m.pricing())
}

func (m *reviewBudgetMeter) reserveForModel(
	estimatedInputTokens int,
	requestedOutputTokens int,
	pricing reviewModelPricing,
) (reviewBudgetReservation, error) {
	if m == nil {
		return reviewBudgetReservation{outputTokens: requestedOutputTokens, pricing: pricing}, nil
	}
	pricing = m.completePricing(pricing)
	if estimatedInputTokens < inputFramingTokenReserve {
		estimatedInputTokens = inputFramingTokenReserve
	}
	if requestedOutputTokens <= 0 {
		return reviewBudgetReservation{}, fmt.Errorf("模型输出 token 上限无效")
	}
	m.mu.Lock()
	remaining := m.limitMicros - m.spentMicros - m.reservedMicros
	inputCost := tokenCostMicros(estimatedInputTokens, pricing.inputPrice)
	if inputCost >= remaining {
		m.mu.Unlock()
		return reviewBudgetReservation{}, fmt.Errorf("审查预算不足，无法继续发送模型请求")
	}
	outputAllowance := int(math.Floor(float64(remaining-inputCost) / pricing.outputPrice))
	if outputAllowance < 1 {
		m.mu.Unlock()
		return reviewBudgetReservation{}, fmt.Errorf("审查预算不足，无法继续生成模型输出")
	}
	if outputAllowance > requestedOutputTokens {
		outputAllowance = requestedOutputTokens
	}
	reserved := inputCost + tokenCostMicros(outputAllowance, pricing.outputPrice)
	m.reservedMicros += reserved
	m.job.ReservedMicros = m.reservedMicros
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return reviewBudgetReservation{}, fmt.Errorf("模型预算预留持久化失败，已阻止模型请求")
	}
	return reviewBudgetReservation{
		reservedMicros: reserved,
		outputTokens:   outputAllowance,
		pricing:        pricing,
	}, nil
}

func (m *reviewBudgetMeter) settle(reservation reviewBudgetReservation, inputTokens, outputTokens int, usageKnown bool) int64 {
	if m == nil {
		return 0
	}
	charge := reservation.reservedMicros
	if usageKnown {
		charge = tokenCostMicros(inputTokens, reservation.pricing.inputPrice) +
			tokenCostMicros(outputTokens, reservation.pricing.outputPrice)
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
	m.job.ReservedMicros = m.reservedMicros
	m.mu.Unlock()
	return charge
}

func (m *reviewBudgetMeter) recoverReservations() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.reservedMicros == 0 {
		m.mu.Unlock()
		return nil
	}
	if m.reservedMicros > math.MaxInt64-m.spentMicros {
		m.spentMicros = math.MaxInt64
	} else {
		m.spentMicros += m.reservedMicros
	}
	m.reservedMicros = 0
	m.job.ReservedMicros = 0
	m.job.SpentMicros = m.spentMicros
	m.job.SpentYuan = model.MicrosToYuan(m.spentMicros)
	m.mu.Unlock()
	return m.persist()
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

func (m *reviewBudgetMeter) pricing() reviewModelPricing {
	if m == nil {
		return reviewModelPricing{}
	}
	return reviewModelPricing{
		modelName:   m.modelName,
		inputPrice:  m.inputPrice,
		outputPrice: m.outputPrice,
	}
}

func (m *reviewBudgetMeter) completePricing(pricing reviewModelPricing) reviewModelPricing {
	if m == nil {
		return pricing
	}
	if pricing.modelName == "" {
		pricing.modelName = m.modelName
	}
	if pricing.inputPrice <= 0 {
		pricing.inputPrice = m.inputPrice
	}
	if pricing.outputPrice <= 0 {
		pricing.outputPrice = m.outputPrice
	}
	return pricing
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
