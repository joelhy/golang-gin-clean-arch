# GORM Models vs Domain Entities: Clean Architecture Best Practices

## ❌ **The Problem: Using GORM Models as Domain Entities**

### What We Had Before (Anti-pattern):

```go
// internal/domain/entities/user.go - WRONG!
package entities

import "gorm.io/gorm" // ❌ Domain depends on infrastructure!

type User struct {
    ID        uint           `gorm:"primarykey"`           // ❌ ORM tags in domain
    Email     string         `gorm:"uniqueIndex;not null"` // ❌ Database constraints in domain
    Name      string         `gorm:"not null"`             // ❌ Infrastructure concerns
    Password  string         `gorm:"not null"`             // ❌ Mixed responsibilities
    CreatedAt time.Time
    UpdatedAt time.Time
    DeletedAt gorm.DeletedAt `gorm:"index"`                // ❌ GORM-specific type in domain
}

func (User) TableName() string { // ❌ Database table name in domain
    return "users"
}
```

### **Problems with This Approach:**

1. **🔗 Tight Coupling**: Domain layer depends on GORM (infrastructure)
2. **🚫 Violates Dependency Inversion**: Inner circle depends on outer circle
3. **🧪 Hard to Test**: Domain logic tied to ORM framework
4. **🔒 Framework Lock-in**: Can't easily switch from GORM to another ORM
5. **🎭 Mixed Responsibilities**: Business logic mixed with persistence concerns
6. **💔 Breaks Clean Architecture**: Domain contaminated with infrastructure details

## ✅ **The Solution: Separated Domain Entities and Data Models**

### **1. Pure Domain Entity:**

```go
// internal/domain/entities/user.go - CORRECT!
package entities

import "time" // ✅ Only standard library dependencies

// Pure domain entity - no external dependencies
type User struct {
    ID        uint
    Email     string
    Name      string
    Password  string
    CreatedAt time.Time
    UpdatedAt time.Time
    DeletedAt *time.Time // ✅ Pure time pointer, no GORM dependency
}

// ✅ Domain factory method with validation
func NewUser(email, name, password string) (*User, error) {
    if email == "" {
        return nil, ErrInvalidEmail
    }
    if name == "" {
        return nil, ErrInvalidName
    }
    if password == "" {
        return nil, ErrInvalidPassword
    }

    return &User{
        Email:     email,
        Name:      name,
        Password:  password,
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }, nil
}

// ✅ Domain behavior methods
func (u *User) IsDeleted() bool {
    return u.DeletedAt != nil
}

func (u *User) MarkAsDeleted() {
    now := time.Now()
    u.DeletedAt = &now
    u.UpdatedAt = now
}

func (u *User) UpdateInfo(name, email string) {
    if name != "" {
        u.Name = name
    }
    if email != "" {
        u.Email = email
    }
    u.UpdatedAt = time.Now()
}

// ✅ Domain-specific errors
var (
    ErrInvalidEmail    = DomainError{Message: "email is required"}
    ErrInvalidName     = DomainError{Message: "name is required"}
    ErrInvalidPassword = DomainError{Message: "password is required"}
    ErrUserNotFound    = DomainError{Message: "user not found"}
    ErrEmailExists     = DomainError{Message: "user with this email already exists"}
)
```

### **2. Infrastructure Data Model:**

```go
// internal/adapters/models/user_model.go - Infrastructure layer
package models

import (
    "time"
    "clean-arch-gin/internal/domain/entities"
    "gorm.io/gorm" // ✅ GORM dependency only in infrastructure layer
)

// GORM model - separate from domain entity
type UserModel struct {
    ID        uint           `gorm:"primarykey"`
    Email     string         `gorm:"uniqueIndex;not null"`
    Name      string         `gorm:"not null"`
    Password  string         `gorm:"not null"`
    CreatedAt time.Time
    UpdatedAt time.Time
    DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (UserModel) TableName() string {
    return "users"
}

// ✅ Mapping methods to convert between layers
func (u *UserModel) ToDomainEntity() *entities.User {
    var deletedAt *time.Time
    if u.DeletedAt.Valid {
        deletedAt = &u.DeletedAt.Time
    }

    return &entities.User{
        ID:        u.ID,
        Email:     u.Email,
        Name:      u.Name,
        Password:  u.Password,
        CreatedAt: u.CreatedAt,
        UpdatedAt: u.UpdatedAt,
        DeletedAt: deletedAt,
    }
}

func (u *UserModel) FromDomainEntity(entity *entities.User) {
    u.ID = entity.ID
    u.Email = entity.Email
    u.Name = entity.Name
    u.Password = entity.Password
    u.CreatedAt = entity.CreatedAt
    u.UpdatedAt = entity.UpdatedAt

    if entity.DeletedAt != nil {
        u.DeletedAt = gorm.DeletedAt{
            Time:  *entity.DeletedAt,
            Valid: true,
        }
    }
}
```

### **3. Repository with Mapping:**

```go
// internal/adapters/repositories/user_repository.go
func (r *userRepository) Create(user *entities.User) error {
    userModel := models.NewUserModelFromEntity(user)

    if err := r.db.Create(userModel).Error; err != nil {
        return err
    }

    // Update entity with generated ID
    user.ID = userModel.ID
    return nil
}

func (r *userRepository) GetByID(id uint) (*entities.User, error) {
    var userModel models.UserModel
    err := r.db.First(&userModel, id).Error
    if err != nil {
        if err == gorm.ErrRecordNotFound {
            return nil, entities.ErrUserNotFound
        }
        return nil, err
    }

    return userModel.ToDomainEntity(), nil // ✅ Convert to domain entity
}
```

### **4. API DTOs (Data Transfer Objects):**

```go
// internal/adapters/controllers/user_controller.go
type UserDTO struct {
    ID        uint      `json:"id"`
    Email     string    `json:"email"`
    Name      string    `json:"name"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
    // ✅ Password excluded from API responses
}

func toDTO(user *entities.User) UserDTO {
    return UserDTO{
        ID:        user.ID,
        Email:     user.Email,
        Name:      user.Name,
        CreatedAt: user.CreatedAt,
        UpdatedAt: user.UpdatedAt,
    }
}
```

## **📊 Comparison Table**

| Aspect | GORM as Entity (❌) | Separated Models (✅) |
|--------|---------------------|----------------------|
| **Domain Purity** | Polluted with ORM tags | Pure business objects |
| **Dependencies** | Domain → Infrastructure | Domain ← Infrastructure |
| **Testability** | Requires GORM setup | Easy unit testing |
| **Framework Independence** | Locked to GORM | ORM agnostic |
| **Business Logic** | Mixed with persistence | Clean separation |
| **API Responses** | Exposes implementation | Clean DTOs |
| **Validation** | Framework-dependent | Domain-driven |
| **Error Handling** | Generic database errors | Domain-specific errors |

## **🏗️ Architecture Layers**

```
📱 API Layer (Controllers)
    ↕️ DTOs
💼 Application Layer (Use Cases)
    ↕️ Domain Entities
🏛️ Domain Layer (Entities, Repositories, Errors)
    ↕️ Domain Entities
🔧 Infrastructure Layer (GORM Models, Database)
    ↕️ Data Models
💾 Database Layer
```

## **✅ Benefits of This Approach**

### **1. True Clean Architecture**
- Domain layer has **zero infrastructure dependencies**
- Proper **dependency inversion** (infrastructure depends on domain)
- **Testable** business logic without external dependencies

### **2. Flexibility**
- **Easy to switch ORMs** (from GORM to Ent, SQLBoiler, etc.)
- **Multiple persistence strategies** (SQL, NoSQL, file, memory)
- **API versioning** through different DTOs

### **3. Better Testing**
```go
// Pure domain testing - no database required
func TestUser_MarkAsDeleted(t *testing.T) {
    user := &entities.User{ID: 1, Name: "John"}
    user.MarkAsDeleted()

    if !user.IsDeleted() {
        t.Error("User should be marked as deleted")
    }
}
```

### **4. Security**
- **Passwords never exposed** in API responses
- **Controlled data exposure** through DTOs
- **Domain validation** ensures data integrity

## **🎯 When to Use This Pattern**

### ✅ **Always Use When:**
- Building applications following Clean Architecture
- Need framework independence
- High test coverage requirements
- Multiple data sources or APIs
- Domain logic complexity
- Team collaboration (clear boundaries)

### ❓ **Consider Simpler Approach When:**
- Very simple CRUD applications
- Rapid prototyping
- Single developer projects
- No business logic complexity

## **📝 Implementation Checklist**

- [ ] ✅ Pure domain entities (no ORM dependencies)
- [ ] ✅ Separate GORM models in infrastructure layer
- [ ] ✅ Mapping methods between entities and models
- [ ] ✅ Domain factory methods and validation
- [ ] ✅ Domain-specific error types
- [ ] ✅ API DTOs for external communication
- [ ] ✅ Repository handles entity ↔ model conversion
- [ ] ✅ Use cases work only with domain entities
- [ ] ✅ Controllers use DTOs for requests/responses

## **🚀 Conclusion**

**Never use GORM models directly as domain entities.** The separation of concerns provided by having distinct domain entities, data models, and DTOs is fundamental to maintainable, testable, and flexible software architecture.

This pattern ensures your business logic remains **pure**, **testable**, and **independent** of infrastructure concerns while providing clear boundaries between layers.