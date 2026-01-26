package order

import "time"

type Order struct {
	ID        uint `gorm:"primaryKey"`
	OrderID   string
	UserID    string
	ProductID string
	Status    string
	CreatedAt time.Time
}
