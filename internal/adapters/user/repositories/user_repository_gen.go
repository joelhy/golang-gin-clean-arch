package repositories

import (
	"clean-arch-gin/internal/adapters/shared/models"
	userEntities "clean-arch-gin/internal/domain/user/entities"
	userRepositories "clean-arch-gin/internal/domain/user/repositories"
	"clean-arch-gin/internal/infrastructure/database/query"

	"gorm.io/gorm"
)

// userRepositoryGen implements UserRepository using GORM Gen
// This provides type-safe, high-performance database operations
type userRepositoryGen struct {
	db    *gorm.DB
	query *query.Query
}

// NewUserRepositoryGen creates a new user repository using GORM Gen
func NewUserRepositoryGen(db *gorm.DB) userRepositories.UserRepository {
	return &userRepositoryGen{
		db:    db,
		query: query.Use(db),
	}
}

// Create creates a new user in the database using GORM Gen
func (r *userRepositoryGen) Create(user *userEntities.User) error {
	userModel := models.NewUserModelFromEntity(user)

	// Use GORM Gen's type-safe Create method
	err := r.query.UserModel.Create(userModel)
	if err != nil {
		return err
	}

	// Update the entity with generated ID
	user.ID = userModel.ID
	return nil
}

// GetByID retrieves a user by ID using GORM Gen
func (r *userRepositoryGen) GetByID(id uint) (*userEntities.User, error) {
	u := r.query.UserModel

	// Type-safe query with GORM Gen (using placeholder for now)
	userModel, err := u.Where(u.ID().Eq(id)).First()
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, userEntities.ErrUserNotFound
		}
		return nil, err
	}

	return userModel.ToDomainEntity(), nil
}

// GetByEmail retrieves a user by email using GORM Gen
func (r *userRepositoryGen) GetByEmail(email string) (*userEntities.User, error) {
	u := r.query.UserModel

	// Type-safe query with GORM Gen (using placeholder for now)
	userModel, err := u.Where(u.Email().Eq(email)).First()
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, userEntities.ErrUserNotFound
		}
		return nil, err
	}

	return userModel.ToDomainEntity(), nil
}

// GetAll retrieves all users with pagination using GORM Gen
func (r *userRepositoryGen) GetAll(limit, offset int) ([]*userEntities.User, error) {
	u := r.query.UserModel

	// Type-safe pagination query with GORM Gen
	userModels, err := u.Limit(limit).Offset(offset).Find()
	if err != nil {
		return nil, err
	}

	// Convert models to entities
	users := make([]*userEntities.User, len(userModels))
	for i, model := range userModels {
		users[i] = model.ToDomainEntity()
	}

	return users, nil
}

// Update updates an existing user using GORM Gen
func (r *userRepositoryGen) Update(user *userEntities.User) error {
	userModel := models.NewUserModelFromEntity(user)
	u := r.query.UserModel

	// Type-safe update with GORM Gen
	_, err := u.Where(u.ID().Eq(user.ID)).Updates(userModel)
	return err
}

// Delete soft deletes a user by ID using GORM Gen
func (r *userRepositoryGen) Delete(id uint) error {
	u := r.query.UserModel

	// Type-safe soft delete with GORM Gen
	_, err := u.Where(u.ID().Eq(id)).Delete()
	return err
}

// Count returns the total number of users using GORM Gen
func (r *userRepositoryGen) Count() (int64, error) {
	u := r.query.UserModel

	// Type-safe count query with GORM Gen
	return u.Count()
}

// Advanced query methods using GORM Gen custom methods

// GetUsersByEmailDomain gets users by email domain using generated method
func (r *userRepositoryGen) GetUsersByEmailDomain(domain string) ([]*userEntities.User, error) {
	u := r.query.UserModel

	// Use GORM Gen's powerful query builder
	userModels, err := u.Where(u.Email().Like("%" + domain)).Find()
	if err != nil {
		return nil, err
	}

	// Convert to domain entities
	users := make([]*userEntities.User, len(userModels))
	for i, model := range userModels {
		users[i] = model.ToDomainEntity()
	}

	return users, nil
}

// GetActiveUsers gets all non-deleted users using GORM Gen
func (r *userRepositoryGen) GetActiveUsers() ([]*userEntities.User, error) {
	u := r.query.UserModel

	// Type-safe query for active users
	userModels, err := u.Where(u.DeletedAt().IsNull()).Find()
	if err != nil {
		return nil, err
	}

	// Convert to domain entities
	users := make([]*userEntities.User, len(userModels))
	for i, model := range userModels {
		users[i] = model.ToDomainEntity()
	}

	return users, nil
}

// GetUsersWithFilters gets users with complex filtering using GORM Gen
func (r *userRepositoryGen) GetUsersWithFilters(limit, offset int, email, name string) ([]*userEntities.User, error) {
	u := r.query.UserModel
	query := u.Select(u.ALL())

	// Build dynamic query with GORM Gen
	if email != "" {
		query = query.Where(u.Email().Like("%" + email + "%"))
	}
	if name != "" {
		query = query.Where(u.Name().Like("%" + name + "%"))
	}

	// Execute with pagination
	userModels, err := query.Limit(limit).Offset(offset).Find()
	if err != nil {
		return nil, err
	}

	// Convert to domain entities
	users := make([]*userEntities.User, len(userModels))
	for i, model := range userModels {
		users[i] = model.ToDomainEntity()
	}

	return users, nil
}
