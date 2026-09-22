package dao

import (
	"fmt"
	"time"

	"CR-Agent/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const teamMessageLeaseDuration = time.Minute

type MySQLTeamMailbox struct {
	db     *gorm.DB
	worker string
}

func NewMySQLTeamMailbox(db *gorm.DB) (*MySQLTeamMailbox, error) {
	worker, err := newRuntimeID("mailbox_", 8)
	if err != nil {
		return nil, err
	}
	return &MySQLTeamMailbox{db: db, worker: worker}, nil
}

func (b *MySQLTeamMailbox) Send(event model.TeamEvent) error {
	if event.ID == "" {
		id, err := newRuntimeID("msg_", 8)
		if err != nil {
			return err
		}
		event.ID = id
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	row := model.DBTeamMessage{
		MessageID:      event.ID,
		JobID:          event.TaskID,
		FromAgent:      event.From,
		ToAgent:        event.To,
		MessageType:    event.Type,
		Content:        event.Content,
		DeliveryStatus: "pending",
		CreatedAt:      event.At,
	}
	if err := b.db.Create(&row).Error; err != nil {
		return fmt.Errorf("发送团队消息: %w", err)
	}
	return nil
}

func (b *MySQLTeamMailbox) Consume(agent, jobID string) ([]model.TeamEvent, error) {
	var rows []model.DBTeamMessage
	err := b.db.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("to_agent = ? AND job_id = ? AND (delivery_status = ? OR (delivery_status = ? AND lease_until < ?))", agent, jobID, "pending", "processing", now).
			Order("created_at asc").Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		leaseUntil := now.Add(teamMessageLeaseDuration)
		ids := make([]uint, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		if err := tx.Model(&model.DBTeamMessage{}).Where("id IN ?", ids).Updates(map[string]any{
			"delivery_status": "processing",
			"lease_owner":     b.worker,
			"lease_until":     leaseUntil,
		}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("消费团队消息: %w", err)
	}
	events := make([]model.TeamEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, model.TeamEvent{
			ID:      row.MessageID,
			From:    row.FromAgent,
			To:      row.ToAgent,
			Type:    row.MessageType,
			TaskID:  row.JobID,
			Content: row.Content,
			At:      row.CreatedAt,
		})
	}
	return events, nil
}

func (b *MySQLTeamMailbox) Ack(events []model.TeamEvent) error {
	if len(events) == 0 {
		return nil
	}
	ids := make([]string, 0, len(events))
	for _, event := range events {
		if event.ID != "" {
			ids = append(ids, event.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC()
	return b.db.Model(&model.DBTeamMessage{}).
		Where("message_id IN ? AND delivery_status = ? AND lease_owner = ?", ids, "processing", b.worker).
		Updates(map[string]any{
			"delivery_status": "acked",
			"consumed_at":     now,
			"lease_owner":     "",
			"lease_until":     nil,
		}).Error
}
