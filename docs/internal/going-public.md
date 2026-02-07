# Going Public
 
I wonder if we should migrate the . I want 
 
Things that are big but I dont think are needed before going public. Let me know if you think different:
 
State - I'm on the fence with this. We really need to discuss this because it may not work as a provider. If it is not a provider, we may need to changes things in a big way
Tests section (in the solution)
Github and GCP Auth-handlers
Github provider
Support for pulling in and using external providers and auth-handlers (Plugins)
The API
 
What comes to mind that is required before we go public:
Go through all the CLI commands and make sure they work as expected, they all feel natural and show the correct data and display it in a good way
We can build scafctl to give it to others on the team to get feed back. I created tons of examples and more importantly, tutorials that they can run through
We may need to spin up a testing namespace in quay
Make sure all the current builtin actions are good enough
I changed the license to Apache but lets review it
Make sure the taskfile is all good
Update the readme
I created a CONTRIBUTING.md file but it needs to be reviewed and updated
Clean the repo of AI configs???
Clean the repo from all existing commits, a single init commit