# GORM Gen Integration: Type-Safe Database Operations

## **🚀 Why GORM Gen?**

GORM Gen takes our Clean Architecture to the next level by providing:

- **🎯 Type Safety**: Compile-time SQL query validation
- **⚡ Better Performance**: Optimized query generation
- **🛡️ SQL Injection Prevention**: Automatically parameterized queries
- **🔍 IntelliSense Support**: Full IDE autocompletion
- **🏗️ Clean Architecture Compatibility**: Maintains separation of concerns

## **📊 GORM Gen vs Traditional GORM**

| Feature | Traditional GORM | GORM Gen |
|---------|------------------|----------|
| **Type Safety** | Runtime errors | Compile-time validation |
| **Performance** | Good | Optimized queries |
| **SQL Injection** | Manual prevention | Automatic prevention |
| **IDE Support** | Limited | Full IntelliSense |
| **Query Complexity** | String-based | Method chaining |
| **Refactoring** | Error-prone | Safe & automated |

## **🏗️ Architecture Integration**

### **Clean Architecture Layers with GORM Gen**

```
┌─────────────────────────────────────────────────────────────┐
│                     🏛️ Domain Layer                        │
│     Pure Entities • Repository Interfaces • Business Logic │
└─────────────────────────────────────────────────────────────┘
                              ↕️
┌─────────────────────────────────────────────────────────────┐
│                   🔌 Adapters Layer                        │
│  Controllers • Use Cases • GORM Models • Repository Impls  │
└─────────────────────────────────────────────────────────────┘
                              ↕️
┌─────────────────────────────────────────────────────────────┐
│                  🔧 Infrastructure Layer                    │
│   Database • GORM Gen Queries • External Services • Config │
└─────────────────────────────────────────────────────────────┘
```

### **File Organization**

```
internal/
├── domain/                           # 🏛️ Pure Domain (unchanged)
│   ├── entities/user.go             # Pure domain entities
│   └── repositories/user_repository.go # Repository interfaces
├── adapters/
│   ├── models/user_model.go         # GORM models with tags
│   └── repositories/
│       ├── user_repository.go       # Traditional GORM impl
│       └── user_repository_gen.go   # 🆕 GORM Gen impl
└── infrastructure/
    └── database/
        ├── gen.go                   # 🆕 GORM Gen configuration
        └── query/                   # 🆕 Generated query code
            ├── query.go             # Generated query methods
            └── user_model.gen.go    # Generated model methods
```

## **⚙️ Setup and Configuration**

### **1. Dependencies**

```go
// go.mod
require (
    gorm.io/gen v0.3.25
    gorm.io/gorm v1.25.5
    gorm.io/driver/mysql v1.5.2
)
```

### **2. Code Generation Configuration**

```go
// internal/infrastructure/database/gen.go
//go:build ignore

package main

import (
    "clean-arch-gin/internal/adapters/models"
    "gorm.io/driver/mysql"
    "gorm.io/gen"
    "gorm.io/gorm"
)

func main() {
    g := gen.NewGenerator(gen.Config{
        OutPath:           "./internal/infrastructure/database/query",
        Mode:              gen.WithoutContext | gen.WithDefaultQuery | gen.WithQueryInterface,
        FieldNullable:     true,
        FieldCoverable:    false,
        FieldSignable:     false,
        FieldWithIndexTag: false,
        FieldWithTypeTag:  true,
    })

    // Connect to database
    db, err := gorm.Open(mysql.Open("DSN"), &gorm.Config{})
    if err != nil {
        panic("failed to connect database")
    }

    g.UseDB(db)

    // Generate for models
    g.ApplyBasic(
        models.UserModel{},
        models.OrderModel{},
        // Add more models...
    )

    // Custom query methods
    user := g.GenerateModel("users")
    g.ApplyInterface(func(UserQueryInterface) {}, user)

    g.Execute()
}
```

### **3. Generated Code Structure**

After running `make gen-query`, you get:

```go
// internal/infrastructure/database/query/query.go (generated)
type Query struct {
    db        *gorm.DB
    UserModel userModelDo
    // Other models...
}

func Use(db *gorm.DB) *Query {
    return &Query{
        db:        db,
        UserModel: newUserModelDo(db),
    }
}
```

## **🔧 Repository Implementation**

### **Traditional GORM vs GORM Gen**

#### **Traditional GORM**
```go
func (r *userRepository) GetByEmail(email string) (*entities.User, error) {
    var userModel models.UserModel
    // ❌ String-based query, runtime errors possible
    err := r.db.Where("email = ?", email).First(&userModel).Error
    if err != nil {
        return nil, err
    }
    return userModel.ToDomainEntity(), nil
}
```

#### **GORM Gen** ✅
```go
func (r *userRepositoryGen) GetByEmail(email string) (*entities.User, error) {
    u := r.query.UserModel
    // ✅ Type-safe query, compile-time validation
    userModel, err := u.Where(u.Email.Eq(email)).First()
    if err != nil {
        return nil, err
    }
    return userModel.ToDomainEntity(), nil
}
```

### **Advanced Query Examples**

#### **Complex Filtering**
```go
func (r *userRepositoryGen) GetUsersWithFilters(filters UserFilters) ([]*entities.User, error) {
    u := r.query.UserModel
    query := u.Select(u.ALL)

    // Dynamic query building with type safety
    if filters.Email != "" {
        query = query.Where(u.Email.Like("%" + filters.Email + "%"))
    }
    if filters.MinAge > 0 {
        query = query.Where(u.Age.Gte(filters.MinAge))
    }
    if filters.Status != "" {
        query = query.Where(u.Status.Eq(filters.Status))
    }
    
    userModels, err := query.Limit(filters.Limit).Offset(filters.Offset).Find()
    if err != nil {
        return nil, err
    }

    return convertToEntities(userModels), nil
}
```

#### **Aggregations and Analytics**
```go
func (r *userRepositoryGen) GetUserStats() (*UserStats, error) {
    u := r.query.UserModel
    
    // Type-safe aggregation queries
    totalUsers, err := u.Count()
    if err != nil {
        return nil, err
    }
    
    activeUsers, err := u.Where(u.Status.Eq("active")).Count()
    if err != nil {
        return nil, err
    }
    
    return &UserStats{
        Total:  totalUsers,
        Active: activeUsers,
    }, nil
}
```

#### **Joins and Relations**
```go
func (r *userRepositoryGen) GetUsersWithOrders() ([]*entities.UserWithOrders, error) {
    u := r.query.UserModel
    o := r.query.OrderModel
    
    // Type-safe joins
    result, err := u.Select(u.ALL, o.TotalAmount.Sum().As("total_spent")).
        LeftJoin(o, u.ID.EqCol(o.UserID)).
        Group(u.ID).
        Find()
    
    if err != nil {
        return nil, err
    }
    
    return convertToUserWithOrders(result), nil
}
```

## **🎯 Benefits in Clean Architecture**

### **1. Maintains Domain Purity**
```go
// Domain layer remains completely pure
type User struct {
    ID    uint
    Email string
    Name  string
    // No GORM tags or dependencies
}
```

### **2. Enhanced Repository Interface**
```go
// Repository interface can now include advanced methods
type UserRepository interface {
    // Basic CRUD
    Create(user *User) error
    GetByID(id uint) (*User, error)
    
    // Advanced type-safe queries
    GetUsersByEmailDomain(domain string) ([]*User, error)
    GetUsersWithFilters(filters UserFilters) ([]*User, error)
    GetUserStats() (*UserStats, error)
}
```

### **3. Better Testing**
```go
func TestUserRepository_GetByEmail(t *testing.T) {
    // Generated code is more testable
    // Type safety prevents many test failures
    // Clear interface contracts
}
```

## **🚀 Development Workflow**

### **1. Initial Setup**
```bash
# Setup database and generate initial code
make setup-db
make gen-query
```

### **2. Adding New Models**
```bash
# 1. Create GORM model in internal/adapters/models/
# 2. Add to gen.go configuration
# 3. Regenerate code
make gen-query
```

### **3. Development Cycle**
```bash
# Daily development
make dev              # Start with hot reload

# After schema changes
make gen-query        # Regenerate queries
make test             # Run tests

# Clean build
make clean-gen        # Clean generated code
make gen-all          # Regenerate everything
```

## **⚡ Performance Benefits**

### **Query Optimization**
```go
// GORM Gen generates optimized queries
user := query.UserModel

// Efficient exists check
exists, err := user.Where(user.Email.Eq(email)).Take()

// Optimized pagination
users, err := user.Limit(10).Offset(20).Find()

// Batch operations
affected, err := user.Where(user.Status.Eq("inactive")).Delete()
```

### **Memory Efficiency**
- Generated code avoids reflection
- Compile-time optimizations
- Reduced memory allocations
- Better garbage collection

## **🛡️ Security Advantages**

### **SQL Injection Prevention**
```go
// ❌ Traditional GORM (vulnerable if not careful)
db.Where("email = '" + userInput + "'")

// ✅ GORM Gen (automatically safe)
user.Where(user.Email.Eq(userInput))
```

### **Type Safety**
```go
// ❌ Runtime error
db.Where("invalid_column = ?", value)

// ✅ Compile-time error
user.Where(user.InvalidColumn.Eq(value)) // Won't compile
```

## **📈 Migration Strategy**

### **Gradual Migration**
```go
// Keep both implementations during transition
func NewUserModule(db *gorm.DB, useGen bool) modules.Module {
    if useGen {
        userRepo := repositories.NewUserRepositoryGen(db)
    } else {
        userRepo := repositories.NewUserRepository(db)
    }
    // ... rest of setup
}
```

### **A/B Testing**
```go
// Compare performance between implementations
type RepositoryMetrics struct {
    QueryTime    time.Duration
    MemoryUsage  int64
    ErrorRate    float64
}

func BenchmarkRepositories(b *testing.B) {
    // Benchmark both implementations
}
```

## **🎛️ Advanced Features**

### **Custom Query Methods**
```go
// Define in gen.go
type UserQueryInterface interface {
    GetTopUsers(limit int) ([]*models.UserModel, error)
    GetUsersByCreatedDate(date time.Time) ([]*models.UserModel, error)
}
```

### **Dynamic Queries**
```go
// Build complex queries dynamically
func (r *userRepositoryGen) SearchUsers(criteria SearchCriteria) ([]*entities.User, error) {
    u := r.query.UserModel
    query := u.Select(u.ALL)
    
    for field, value := range criteria.Filters {
        switch field {
        case "email":
            query = query.Where(u.Email.Like("%" + value + "%"))
        case "name":
            query = query.Where(u.Name.Like("%" + value + "%"))
        case "status":
            query = query.Where(u.Status.Eq(value))
        }
    }
    
    return query.Find()
}
```

## **🔧 Troubleshooting**

### **Common Issues**

1. **Generation Fails**
   ```bash
   # Ensure database is running and accessible
   make setup-db
   # Check database connection in gen.go
   ```

2. **Import Errors**
   ```bash
   # Clean and regenerate
   make clean-gen
   make gen-query
   ```

3. **Type Errors**
   ```bash
   # Update model definitions
   # Regenerate after model changes
   make gen-query
   ```

## **📊 Best Practices**

### **1. Keep Domain Pure**
- Never import GORM Gen in domain layer
- Always convert between models and entities
- Maintain clear boundaries

### **2. Use Type Safety**
- Prefer generated methods over raw queries
- Leverage compile-time validation
- Use IDE autocompletion

### **3. Performance Optimization**
- Use generated batch operations
- Leverage query optimization
- Monitor generated SQL

### **4. Testing Strategy**
- Test repository interfaces, not implementations
- Use dependency injection for testing
- Mock at the repository level

## **🚀 Conclusion**

GORM Gen enhances our Clean Architecture by providing:

- **Better Performance**: Optimized, type-safe queries
- **Enhanced Safety**: Compile-time validation and SQL injection prevention
- **Improved DX**: Better IDE support and debugging
- **Maintained Purity**: Domain layer remains unchanged
- **Future-Proof**: Easy to extend and maintain

The integration maintains all Clean Architecture principles while providing modern, type-safe database operations that scale with your application's complexity. 