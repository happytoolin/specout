package main

import (
	"net/http"

	"github.com/happytoolin/specout"
)

// op assembles one operation from its published metadata, the
// learn.microsoft.com page slug ("" when the document has none) and the body.
func op[Req, Res any](summary, description, id, tag, docs string, responses []specout.Response, h http.HandlerFunc) specout.Handler[Req, Res] {
	handler := specout.Handler[Req, Res]{HandlerFunc: h, Summary: summary, Description: description, OperationID: id, Tags: []string{tag}, Responses: responses}
	if docs != "" {
		handler.ExternalDocs = &specout.ExternalDocs{
			Description: "Find more info here",
			URL:         "https://learn.microsoft.com/graph/api/" + docs + "?view=graph-rest-1.0",
		}
	}
	return handler
}

// routes registers the eight published operations on b.
func routes(b *specout.GorillaRouter, s *store) {
	b.Get("/me", op[getMeReq, User]("Get a user", "Retrieve the properties and relationships of user object.",
		"me.user.GetUser", "me.user", "user-get", ok("Retrieved entity"), s.getMe))
	b.Get("/users", op[listUsersReq, UserCollectionResponse]("List users", "Retrieve a list of user objects.",
		"users.user.ListUser", "users.user", "user-list", ok("Retrieved collection"), s.listUsers))
	b.Post("/users", op[User, User]("Create User", "Create a new user.",
		"users.user.CreateUser", "users.user", "user-post-users", ok("Created entity"), s.createUser))
	b.Get("/users/{user-id}", op[getUserReq, User]("Get a user", "Retrieve the properties and relationships of user object.",
		"users.user.GetUser", "users.user", "user-get", ok("Retrieved entity"), s.getUser))
	b.Patch("/users/{user-id}", op[User, User]("Update user", "Update the properties of a user object.",
		"users.user.UpdateUser", "users.user", "user-update", ok("Success"), s.patchUser))
	b.Delete("/users/{user-id}", op[deleteUserReq, specout.NoContent]("Delete a user", "Delete a user object.",
		"users.user.DeleteUser", "users.user", "user-delete", noContent(), s.deleteUser))
	b.Get("/users/{user-id}/photo/$value", op[mediaReq, struct{}]("Get media content for the navigation property photo from users",
		"The user's profile photo. Read-only.", "users.GetPhotoContent", "users.profilePhoto", "",
		withErrs(specout.Response{Status: 200, Key: "2XX", ContentType: "application/octet-stream", Raw: map[string]any{"description": "Retrieved media content"}}), s.getPhoto))
	b.Post("/users/{user-id}/sendMail", op[sendMailReq, specout.NoContent]("Invoke action sendMail",
		"Send the message specified in the request body using either JSON or MIME format.", "users.user.sendMail", "users.user.Actions", "user-sendmail", noContent(), s.sendMail))
}
