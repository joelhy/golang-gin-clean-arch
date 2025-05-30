package repositories

import (
	"clean-arch-gin/internal/adapters/models"
	userEntities "clean-arch-gin/internal/domain/user/entities"
	userRepositories "clean-arch-gin/internal/domain/user/repositories"

	"gorm.io/gorm"
)

// userRepository implements UserRepository interface using traditional GORM
type userRepository struct {
	db *gorm.DB
}

// NewUserRepository creates a new user repository
func NewUserRepository(db *gorm.DB) userRepositories.UserRepository {
	return &userRepository{db: db}
}

// Create creates a new user in the database
func (r *userRepository) Create(user *userEntities.User) error {
	userModel := models.NewUserModelFromEntity(user)
	if err := r.db.Create(userModel).Error; err != nil {
		return err
	}
	user.ID = userModel.ID
	return nil
}

// GetByID retrieves a user by ID
func (r *userRepository) GetByID(id uint) (*userEntities.User, error) {
	var userModel models.UserModel
	err := r.db.First(&userModel, id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, userEntities.ErrUserNotFound
		}
		return nil, err
	}
	return userModel.ToDomainEntity(), nil
}

// GetByEmail retrieves a user by email
func (r *userRepository) GetByEmail(email string) (*userEntities.User, error) {
	var userModel models.UserModel
	err := r.db.Where("email = ?", email).First(&userModel).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, userEntities.ErrUserNotFound
		}
		return nil, err
	}
	return userModel.ToDomainEntity(), nil
}

// GetAll retrieves all users with pagination
func (r *userRepository) GetAll(limit, offset int) ([]*userEntities.User, error) {
	var userModels []models.UserModel
	err := r.db.Limit(limit).Offset(offset).Find(&userModels).Error
	if err != nil {
		return nil, err
	}

	users := make([]*userEntities.User, len(userModels))
	for i, model := range userModels {
		users[i] = model.ToDomainEntity()
	}
	return users, nil
}

// Update updates an existing user
func (r *userRepository) Update(user *userEntities.User) error {
	userModel := models.NewUserModelFromEntity(user)
	return r.db.Save(userModel).Error
}

// Delete soft deletes a user by ID
func (r *userRepository) Delete(id uint) error {
	return r.db.Delete(&models.UserModel{}, id).Error
}

// Count returns the total number of users
func (r *userRepository) Count() (int64, error) {
	var count int64
	err := r.db.Model(&models.UserModel{}).Count(&count).Error
	return count, err
}

// GetUsersByEmailDomain gets users by email domain (traditional implementation)
func (r *userRepository) GetUsersByEmailDomain(domain string) ([]*userEntities.User, error) {
	var userModels []models.UserModel
	err := r.db.Where("email LIKE ?", "%"+domain).Find(&userModels).Error
	if err != nil {
		return nil, err
	}

	users := make([]*userEntities.User, len(userModels))
	for i, model := range userModels {
		users[i] = model.ToDomainEntity()
	}
	return users, nil
}

// GetActiveUsers gets all non-deleted users (traditional implementation)
func (r *userRepository) GetActiveUsers() ([]*userEntities.User, error) {
	var userModels []models.UserModel
	err := r.db.Where("deleted_at IS NULL").Find(&userModels).Error
	if err != nil {
		return nil, err
	}

	users := make([]*userEntities.User, len(userModels))
	for i, model := range userModels {
		users[i] = model.ToDomainEntity()
	}
	return users, nil
}

// GetUsersWithFilters gets users with complex filtering (traditional implementation)
func (r *userRepository) GetUsersWithFilters(limit, offset int, email, name string) ([]*userEntities.User, error) {
	var userModels []models.UserModel
	query := r.db.Model(&models.UserModel{})

	if email != "" {
		query = query.Where("email LIKE ?", "%"+email+"%")
	}
	if name != "" {
		query = query.Where("name LIKE ?", "%"+name+"%")
	}

	err := query.Limit(limit).Offset(offset).Find(&userModels).Error
	if err != nil {
		return nil, err
	}

	users := make([]*userEntities.User, len(userModels))
	for i, model := range userModels {
		users[i] = model.ToDomainEntity()
	}
	return users, nil
}
