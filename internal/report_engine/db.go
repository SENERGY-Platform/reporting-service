package report_engine

import (
	"context"
	"fmt"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"report-service/internal/helper"
	"time"
)

var DB *mongo.Client
var CTX mongo.SessionContext

func InitDB() {
	CTX, _ := context.WithTimeout(context.Background(), 10*time.Second)
	client, err := mongo.Connect(CTX, options.Client().ApplyURI(helper.GetEnv("MONGODB_URI", "mongodb://localhost:27017")))
	if err != nil {
		panic("failed to connect database: " + err.Error())
	} else {
		fmt.Println("Connected to DB.")
	}
	DB = client
}

func Mongo() *mongo.Collection {
	return DB.Database("reporting").Collection("reports")
}

func CloseDB() {
	err := DB.Disconnect(CTX)
	if err != nil {
		panic("failed to disconnect database: " + err.Error())
	}
}

func GetDB() *mongo.Client {
	return DB
}
