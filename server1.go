import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

)
//URL
var mongoURL string = "mongodb://localhost:27017"

// Meeting structure

type Meeting struct {
	ID                primitive.ObjectID  `json:"_id" bson:"_id"`
	Title             string              `json:"title" bson:"title"`
	Participants      []Participant       `json:"participants" bson:"participants"`
	StartTime         primitive.Timestamp `json:"startTime" bson:"startTime"`
	EndTime           primitive.Timestamp `json:"endTime" bson:"endTime"`
	CreationTimestamp primitive.Timestamp `json:"createdAt" bson:"createdAt"`
}

//Participant structure
type Participant struct {
	Name  string `json:"name" bson:"name"`
	Email string `json:"email" bson:"email"`
	RSVP  string `json:"rsvp" bson:"rsvp"`
}

type NewMeeting struct {
	Title             string              `json:"title" bson:"title"`
	Participants      []Participant       `json:"participants" bson:"participants"`
	StartTime         string              `json:"startTime" bson:"startTime"`
	EndTime           string              `json:"endTime" bson:"endTime"`
	CreationTimestamp primitive.Timestamp `json:"createdAt" bson:"createdAt"`
}

// Database fro New Meeting Created is ...
type NewMeetngForDB struct {
	Title             string              `json:"title" bson:"title"`
	Participants      []Participant       `json:"participants" bson:"participants"`
	StartTime         primitive.Timestamp `json:"startTime" bson:"startTime"`
	EndTime           primitive.Timestamp `json:"endTime" bson:"endTime"`
	CreationTimestamp primitive.Timestamp `json:"createdAt" bson:"createdAt"`
}

// handler interface
type HandleFuncType struct {
	client *mongo.Client
	mux    sync.Mutex				//mutual exclusion
}

//Error Handling
type Error struct{
	StatusCode int		`json:"status_code"`
	ErrorMessage string	`json:"error_message"`
}

//invalid request response writer function
func invalid_request(w http.ResponseWriter, errorCode int, message string){
	w.Header().Set("Content-Type", "application/json")
	switch errorCode {
	case 200: w.WriteHeader(http.StatusOK)
	case 400: w.WriteHeader(http.StatusBadRequest)
	case 403: w.WriteHeader(http.StatusForbidden)
	case 404: w.WriteHeader(http.StatusNotFound)
	case 500: w.WriteHeader(http.StatusInternalServerError)
	default: w.WriteHeader(http.StatusNotFound)
	}
	err := Error {
							StatusCode: errorCode,
							ErrorMessage: message}
	json.NewEncoder(w).Encode(err)
}


func (h *HandleFuncType) meetingManager(w http.ResponseWriter, r *http.Request) {
	
	//break the url to get meetings/meeting domain
	parts := strings.Split(r.URL.Path, "/")
	if parts[1] == "meeting" && len(parts) == 3 {
		if r.Method == "GET" { 																				// Get meeting by ID
			id := r.URL.Path[len("/meeting/"):]
			response, statusCode := h.getMeetingByID(id)
			//Error Handling for meeting ID
			if statusCode != 200 {
				w.WriteHeader(statusCode)
			} else {
				w.WriteHeader(200)
				w.WriteHeader("Ok Success")
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)
			}
		}
	} else if parts[1] == "meetings" {
		if r.Method == "POST" { 																			// New Schedule Meeting
			//Error Handling for JSON
			if r.Header.Get("Content-Type") != "application/json" {
				w.WriteHeader(400)
				invalid_request(w,400,"This end point accepts only JSON request body")
				return
			}
			// Use http.MaxBytesReader to enforce a maximum read of 1MB from the response body. 
			r.Body = http.MaxBytesReader(w, r.Body, 1048576)
			var meetingDetails = NewMeeting{
				CreationTimestamp: primitive.Timestamp{T: uint32(time.Now().Unix())},
			}
			err := json.NewDecoder(r.Body).Decode(&meetingDetails)
			//Error Handling for JSON
			if err != nil {
				invalid_request(w,400,"This is Bad Request!  ")
				w.WriteHeader(400)
				fmt.Println(err)
				return
			}		
																											//Now Check For Collision
			colliding, possibleStatusCode := h.getMeetingCollision(meetingDetails.Participants, meetingDetails.StartTime, meetingDetails.EndTime)
			//Handling Collision
			if possibleStatusCode == 200 {
				if colliding {
					invalid_request(w, 403, "This Meeting Exists..")
					w.WriteHeader(403)
					return
				}
				
				response, statusCode := h.scheduleNewMeeting(meetingDetails)
				w.WriteHeader(statusCode)
				if statusCode==200{
					w.WriteHeader("Ok Success")
				}
				else{
					w.WriteHeader(statusCode)
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)																		//Return JSON 
				return
			}
			w.WriteHeader(possibleStatusCode)
		} else if r.Method == "GET" { 															// Get meeting by time range and email
			
			keys := r.URL.Query()
			//Checking URL for  ‘/meetings?participant=<email id>’
			if len(keys)==0 {
				w.WriteHeader("Not a valid query at this end point")
			}if email, ok := keys["participant"]; !ok || len(email[0])<1{
				invalid_request(w, 400, "Not a valid query at this end point")
			}
			else{
				r.ParseForm()
			var limit, offset int64
			var err error
			if len(r.Form["offset"]) != 0 {
				offset, err = strconv.ParseInt(r.Form["offset"][0], 10, 64)
				if err != nil {
					offset = 0
				}
			}
			if len(r.Form["limit"]) != 0 {
				limit, err = strconv.ParseInt(r.Form["limit"][0], 10, 64)
				if err != nil {
					limit = 10
				}
			}
			response, statusCode := h.getMeetingByEmailAndTimeRange(r.Form["participant"], r.Form["start"], r.Form["end"], limit, offset)
			w.WriteHeader(statusCode)
			json.NewEncoder(w).Encode(response)
			}
		}
		else if r.Method == "GET" {																		// Get meeting by time range 
			keys := r.URL.Query()
			//Checking URL for ‘/meetings?start=<start time here>&end=<end time here>’
			if len(keys)==0 {
				w.WriteHeader("Not a valid query at this end point")
			}
			start, okStart := keys["start"]
			end, okEnd := keys["end"]
			//check both start and end time are provided, else error
			if !okStart || !okEnd {invalid_request(w, 400, "Not a valid query at this end point")
			}else{
				r.ParseForm()
				response, statusCode := h.getMeetingByTimeRange(r.Form["start"], r.Form["end"], limit, offset)
				w.WriteHeader(statusCode)
				json.NewEncoder(w).Encode(response)
			}
			
			
			
		}
	}
}

																										//Schedule a new meeting 
func (h *HandleFuncType) scheduleNewMeeting(meetingDetails NewMeeting) (Meeting, int) {
	h.mux.Lock()
	defer h.mux.Unlock()
	var result Meeting
	startTime, err := time.Parse(time.RFC3339, meetingDetails.StartTime)
	endTime, err := time.Parse(time.RFC3339, meetingDetails.EndTime)
	if err != nil {
		fmt.Println("Invalid Date format", err)
		return result, 400
	}
	var meetingDetailsToDB = NewMeetngForDB{
		Title:             meetingDetails.Title,
		Participants:      meetingDetails.Participants,
		StartTime:         primitive.Timestamp{T: uint32(startTime.Unix())},
		EndTime:           primitive.Timestamp{T: uint32(endTime.Unix())},
		CreationTimestamp: meetingDetails.CreationTimestamp,
	}
	collection := h.client.Database("main").Collection("meetings")
	res, err := collection.InsertOne(context.TODO(), meetingDetailsToDB)
	if err != nil {
		fmt.Println("Insert failed", err)
		invalid_request(w, 500, "Internal Server Error")
		return result, 500
	}
	oid, _ := res.InsertedID.(primitive.ObjectID)
	return Meeting{
		ID:                oid,
		Title:             meetingDetailsToDB.Title,
		Participants:      meetingDetailsToDB.Participants,
		StartTime:         meetingDetailsToDB.StartTime,
		EndTime:           meetingDetailsToDB.EndTime,
		CreationTimestamp: meetingDetailsToDB.CreationTimestamp,
	}, 200
}

																												// Get meeting ID

func (h *HandleFuncType) getMeetingByID(meetingID string) (Meeting, int) {
	var result Meeting
	docID, err := primitive.ObjectIDFromHex(meetingID)
	if err != nil {
		fmt.Println("Invalid ID", err)
		return result, 400
	}
	collection := h.client.Database("main").Collection("meetings")
	response := collection.FindOne(context.TODO(), bson.M{"_id": docID})
	if response == nil {
		fmt.Println("Not found", err)
		return result, 404
	}
	err = response.Decode(&result)
	if err != nil {
		fmt.Println("Bad data", err)
		return result, 500
	}
	return result, 200
}

																								// Get Meeting By Email And TimeRange
func (h *HandleFuncType) getMeetingByEmailAndTimeRange(email []string, startTime []string, endTime []string, limit int64, offset int64) ([]Meeting, int) {
	var result []Meeting
	var emailGiven string = ""
	if len(email) != 0 {
		emailGiven = email[0]
	}
	var startTimeGiven string = ""
	if len(startTime) != 0 {
		startTimeGiven = startTime[0]
	}
	var endTimeGiven string = ""
	if len(endTime) != 0 {
		endTimeGiven = endTime[0]
	}

	startT, err := time.Parse(time.RFC3339, startTimeGiven)
	endT, err := time.Parse(time.RFC3339, endTimeGiven)
	var filter bson.D
	if err != nil {
		filter = bson.D{{"participants", bson.D{{"$elemMatch", bson.D{{"email", emailGiven}}}}}}
	} else {
		if startTimeGiven != "" && endTimeGiven != "" && emailGiven != "" {
			start := primitive.Timestamp{T: uint32(startT.Unix())}
			end := primitive.Timestamp{T: uint32(endT.Unix())}
			filter = bson.D{{"startTime", bson.D{{"$gte", start}}}, {"endTime", bson.D{{"$lte", end}}}, {"participants", bson.D{{"$elemMatch", bson.D{{"email", emailGiven}}}}}}
		} else if emailGiven != "" {
			filter = bson.D{{"participants", bson.D{{"$elemMatch", bson.D{{"email", emailGiven}}}}}}
		} else if startTimeGiven != "" && endTimeGiven != "" {
			start := primitive.Timestamp{T: uint32(startT.Unix())}
			end := primitive.Timestamp{T: uint32(endT.Unix())}
			filter = bson.D{{"startTime", bson.D{{"$gte", start}}}, {"endTime", bson.D{{"$lte", end}}}}
		} else {
			filter = bson.D{}
		}
	}
	options := options.Find()
	options.SetSort(bson.D{{"startTime", 1}})
	options.SetLimit(limit)
	options.SetSkip(offset)
	collection := h.client.Database("main").Collection("meetings")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cur, err := collection.Find(ctx, filter, options)
	if err != nil {
		fmt.Println("Error", err)
		return result, 500
	}
	for cur.Next(ctx) {
		var res Meeting
		err := cur.Decode(&res)
		result = append(result, res)
		if err != nil {
			fmt.Println("Bad DB!", err)
			return result, 500
		}
	}
	return result, 200
}

																									// Get Meeting By TimeRange
func (h *HandleFuncType) getMeetingByTimeRange(startTime string, endTime string, limit int64, offset int64) ([]Meeting, int) {
	var result []Meeting
	startT, err := time.Parse(time.RFC3339, startTime)
	endT, err := time.Parse(time.RFC3339, endTime)
	if err != nil {
		fmt.Println("Invalid Date format", err)
		return result, 400
	}
	start := primitive.Timestamp{T: uint32(startT.Unix())}
	end := primitive.Timestamp{T: uint32(endT.Unix())}
	options := options.Find()
	options.SetSort(bson.D{{"startTime", 1}})
	options.SetLimit(limit)
	options.SetSkip(offset)
	collection := h.client.Database("main").Collection("meetings")
	cur, err := collection.Find(
		context.TODO(),
		bson.D{{"startTime", bson.D{{"$gte", start}}}, {"endTime", bson.D{{"$lte", end}}}},
		options,
	)
	if err != nil {
		fmt.Println("Not found!", err)
		return result, 500
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for cur.Next(ctx) {
		var res Meeting
		err := cur.Decode(&res)
		result = append(result, res)
		if err != nil {
			fmt.Println("Bad DataBase!", err)
			return result, 500
		}
	}
	return result, 200
}
func (h *HandleFuncType) ServeHTTP(w http.ResponseWriter, r *http.Request){
	h.meetingManager(w,r)
}

func main() {																								//Main method 
	PORT := ":9090"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURL))											
	if err != nil {
		log.Fatal(err)
	}
	err = client.Ping(ctx, nil)																				
	if err != nil {
		log.Fatal(err)
	}																										

	Handler := &HandleFuncType{
		client: client,
	}
	http.Handle("/", Handler)

	http.ListenAndServe(PORT, Handler)
	defer func() {
		if err := client.Disconnect(ctx); err != nil {
			panic(err)
		}
	}()
}


												

func (h *HandleFuncType) getMeetingCollision(participants []Participant, startTime string, endTime string) (bool, int) {
	h.mux.Lock()																		//Here we Check for COllsion of meetings
	defer h.mux.Unlock()
	startT, err := time.Parse(time.RFC3339, startTime)
	endT, err := time.Parse(time.RFC3339, endTime)
	if err != nil {
		fmt.Println("Invalid Date format", err)
		return false, 400
	}
	start := primitive.Timestamp{T: uint32(startT.Unix())}
	end := primitive.Timestamp{T: uint32(endT.Unix())}
	var emails []string
	for _, p := range participants {
		if p.RSVP == "Yes" {
			emails = append(emails, p.Email)
		}
	}
	collection := h.client.Database("main").Collection("meetings")
	response := collection.FindOne(
		context.TODO(),
		bson.D{
			{"participants", bson.D{
				{"$elemMatch", bson.D{
					{"rsvp", "Yes"},
					{"email", bson.D{
						{"$in", emails},
					}},
				}},
			}},
			{"$or", bson.A{
				bson.D{
					{"startTime", bson.D{
						{"$gte", start},
						{"$lt", end},
					}},
				},
				bson.D{
					{"endTime", bson.D{
						{"$gte", start},
						{"$lt", end},
					}},
				}},
			},
		},
	)
	if response == nil {
		return false, 500
	}
	var result Meeting
	err = response.Decode(&result)
	if err != nil {
		return false, 200
	}
	return true, 200
}
