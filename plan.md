ID5 IRL Attendance App
Overview
A simple web app for collecting attendee information ahead of company offsites (IRLs). Admins configure the event once; employees submit their details via a form. The app handles conditional logic, reminders, and exports.

1. Admin Setup
Each event is configured by an admin with the following fields:

IRL event name (e.g. "IRL Dubrovnik October 2026")
Event dates (Start date, End date), each day of a type: travel day, event day, default is first and last day are travel days
Location, Country and city
Hotel name and address
Submission deadline
Number of days before submission deadline to send daily reminders, default is 3

2. Attendee Form
Step 1 — Basic details
First name, last name
Attending? Yes / No / Not sure (this means an employee cannot give yes or no before the submission deadline, this needs a reason why)

If No → Display instructions
Show the following message to the attendee:
If for any reason you cannot attend this offsite, please follow the steps below:
Let your manager know
Inform the People team by emailing people@id5.io


If Yes → Show the following fields
Travel details
Arrival: day, time, flight number or information about other travel types, car, train, ??
Departure: day, time, flight number or information about other travel types, car, train, ??
Are you a long-haul traveller (international flight of 7+ hours)?  Yes / No
If Yes: Would you require an extra night on Sunday?  Yes / No
Other
Allergies / dietary preferences
Comments

3. Functional Requirements
Edit & notifications: Attendees can edit their submission after submitting. Admin should receive an email notification whenever someone changes their data.
Access: Each employee logs in via google sso. Each event has a shareable event URL. Access is  restricted to @id5.io email addresses so only company staff can access it.
Employee list: Admin uploads a simple CSV (name + work email) at the start of each event. This list is used for non-responder tracking only — not for sending individual invites.
Dashboard & export: Admins need an export function (CSV is fine). A response dashboard is also essential.
Non-responder tracking: Using the uploaded employee list, the dashboard shows how many people have not yet submitted and lists them by name. All dashboard reload every minute (dropdown 5s, 15s, 1m, 5m, off)
Reminders: The app should send automated reminder emails to non-responders — it sends 1 email per week plus the configurable emails prior to the deadline. Admin should be able to configure the timing.
