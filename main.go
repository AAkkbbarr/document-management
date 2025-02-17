package main

import (
	"database/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

type Document struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	Description string `json:"description"`
	CategoryID  *int   `json:"category_id"`
}

type Category struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

var db *sql.DB

func main() {
	// Создаем необходимые директории
	os.MkdirAll("./uploads", 0755)
	os.MkdirAll("./templates", 0755)

	// Подключение к базе данных
	var err error
	db, err = sql.Open("postgres", "host=localhost user=postgres password=postgres dbname=docstore sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}

	// Создание таблиц
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS categories (
            id SERIAL PRIMARY KEY,
            name VARCHAR(100) NOT NULL UNIQUE
        );

        CREATE TABLE IF NOT EXISTS documents (
            id SERIAL PRIMARY KEY,
            name VARCHAR(255) NOT NULL,
            type VARCHAR(100),
            path VARCHAR(500),
            size BIGINT,
            description TEXT,
            category_id INTEGER REFERENCES categories(id)
        );
    `)
	if err != nil {
		log.Fatal(err)
	}

	router := gin.Default()

	// Маршруты
	router.LoadHTMLFiles("templates/index.html")
	router.Static("/static", "./static")

	router.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", nil)
	})

	// API endpoints
	router.GET("/api/documents", getDocuments)
	router.POST("/api/documents", uploadDocument)
	router.DELETE("/api/documents/:id", deleteDocument)
	router.GET("/api/documents/:id/download", downloadDocument)
	router.GET("/api/categories", getCategories)
	router.POST("/api/categories", createCategory)
	router.DELETE("/api/categories/:id", deleteCategory)

	router.Run(":8080")
}

func getDocuments(c *gin.Context) {
	search := c.Query("search")
	categoryID := c.Query("category")

	query := `SELECT id, name, type, path, size, description, category_id FROM documents WHERE 1=1`
	var params []interface{}

	if search != "" {
		query += ` AND (name ILIKE $1 OR description ILIKE $1)`
		params = append(params, "%"+search+"%")
	}

	if categoryID != "" {
		if len(params) == 0 {
			query += ` AND category_id = $1`
		} else {
			query += ` AND category_id = $2`
		}
		params = append(params, categoryID)
	}

	rows, err := db.Query(query, params...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var documents []Document
	for rows.Next() {
		var doc Document
		err := rows.Scan(&doc.ID, &doc.Name, &doc.Type, &doc.Path, &doc.Size, &doc.Description, &doc.CategoryID)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		documents = append(documents, doc)
	}

	c.JSON(200, documents)
}

func uploadDocument(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Получаем остальные данные
	description := c.PostForm("description")
	categoryIDStr := c.PostForm("category_id")

	// Сохраняем файл
	filename := filepath.Join("uploads", file.Filename)
	if err := c.SaveUploadedFile(file, filename); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// Подготавливаем category_id
	var categoryID *int
	if categoryIDStr != "" {
		id, err := strconv.Atoi(categoryIDStr)
		if err == nil {
			categoryID = &id
		}
	}

	// Сохраняем в базу
	_, err = db.Exec(`
        INSERT INTO documents (name, type, path, size, description, category_id)
        VALUES ($1, $2, $3, $4, $5, $6)
    `, file.Filename, file.Header.Get("Content-Type"), filename, file.Size, description, categoryID)

	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "success"})
}

func deleteDocument(c *gin.Context) {
	id := c.Param("id")

	var filepath string
	err := db.QueryRow("SELECT path FROM documents WHERE id = $1", id).Scan(&filepath)
	if err != nil {
		c.JSON(404, gin.H{"error": "Document not found"})
		return
	}

	// Удаляем файл
	os.Remove(filepath)

	// Удаляем запись
	_, err = db.Exec("DELETE FROM documents WHERE id = $1", id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "success"})
}

func getCategories(c *gin.Context) {
	rows, err := db.Query("SELECT id, name FROM categories ORDER BY name")
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var categories []Category
	for rows.Next() {
		var cat Category
		err := rows.Scan(&cat.ID, &cat.Name)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		categories = append(categories, cat)
	}

	c.JSON(200, categories)
}

func createCategory(c *gin.Context) {
	var category Category
	if err := c.BindJSON(&category); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	err := db.QueryRow(
		"INSERT INTO categories (name) VALUES ($1) RETURNING id",
		category.Name,
	).Scan(&category.ID)

	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, category)
}

func deleteCategory(c *gin.Context) {
	id := c.Param("id")

	// Обновляем документы, убирая ссылку на категорию
	_, err := db.Exec("UPDATE documents SET category_id = NULL WHERE category_id = $1", id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// Удаляем категорию
	_, err = db.Exec("DELETE FROM categories WHERE id = $1", id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "success"})
}
func downloadDocument(c *gin.Context) {
	id := c.Param("id")

	// Получаем информацию о файле из базы данных
	var filename string
	var filepath string
	err := db.QueryRow("SELECT name, path FROM documents WHERE id = $1", id).Scan(&filename, &filepath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Document not found"})
		return
	}

	// Проверяем существование файла
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}

	// Отправляем файл клиенту
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.File(filepath)
}
