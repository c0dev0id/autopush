# automatically push new commits

autopush can be started in your git repository, it will (pseudocode):

LOOP
  IF new commit has been created
    DO git push
    IF push == successful
      check if push workflows are configured
      IF yes
        observe workflow status
        IF successful
          continue
        ELSE
          show error + failed workflow link
          continue
        END IF
      END IF
    ELSE
      show error
    END IF
  END IF
END LOOP

autopush notifies you using multiple mechanisms:
- it shows the current status on stdout
- it shows the current status in the X-window title
- it shows the current status in the tmux status bar

It only consumes Github API tokens while it's checking for the workflow status. Once the workflow is done (failed or ok), it will stop querying github and wait patiently for the next commit (that will  surely fix the situation).

If a git push goes wrong for any reason (detached head, remote has unpulled commits etc), autopush will observe the git directory. You need to manually resolve the situation. Autopush will automatically take over, once the repository is clean again and new commits are detected.
