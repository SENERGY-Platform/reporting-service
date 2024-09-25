package report_engine

import (
	"context"
	"fmt"
	"report-service/internal/helper"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var DB *mongo.Client
var CTX mongo.SessionContext

func InitDB() {
	CTX, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(CTX, options.Client().ApplyURI(helper.GetEnv("MONGODB_URI", "mongodb://localhost:27017")))
	if err != nil {
		if CTX.Err() == context.DeadlineExceeded {
			// handle the case where the context was cancelled due to the timeout
			fmt.Println("context cancelled due to timeout")
		} else {
			panic("failed to connect database: " + err.Error())
		}
	} else {
		fmt.Println("Connected to DB.")
	}
	DB = client
}

func Reports() *mongo.Collection {
	return DB.Database("reporting").Collection("reports")
}

func CloseDB() {
	err := DB.Disconnect(CTX)
	if err != nil {
		panic("failed to disconnect database: " + err.Error())
	}
}
