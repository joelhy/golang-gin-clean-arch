// Command generate produces typed GORM Gen queries from the handwritten models.
package main

import (
	"clean-arch-gin/mysqlstore/model"

	"gorm.io/gen"
)

func main() {
	generator := gen.NewGenerator(gen.Config{
		OutPath:      "../query",
		ModelPkgPath: "../model",
		Mode:         gen.WithDefaultQuery | gen.WithQueryInterface,
	})
	generator.ApplyBasic(
		model.User{},
		model.Role{},
		model.Permission{},
		model.UserRole{},
		model.RolePermission{},
		model.Session{},
		model.RefreshToken{},
		model.Product{},
		model.StockAdjustment{},
		model.Order{},
		model.OrderItem{},
		model.IdempotencyKey{},
	)
	generator.Execute()
}
