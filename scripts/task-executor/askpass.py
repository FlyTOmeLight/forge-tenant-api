#!/opt/executor/bin/python
import os,sys
print('oauth2' if 'username' in sys.argv[1].lower() else os.environ['GITLAB_TOKEN'])
