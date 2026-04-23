# Disk Pressure

## Trigger

- disk usage crosses the configured warning threshold
- inode pressure or free-space exhaustion threatens library, download, or transcode paths

## Evidence

- affected filesystem and mount source
- free-space and inode percentages
- largest local contributors
- transcode and download path usage

## Safe Actions

- open or update a disk-pressure incident
- classify temp, log, and transcode usage
- perform strict allowlisted cleanup only for temp, log, or transcode paths

## Approval-Required Actions

- deleting downloads
- deleting media
- moving library roots
- resizing or reconfiguring storage

## Rollback Trigger

- disk pressure appeared after an approved maintenance change
- approved cleanup changed the stack state and introduced new failures

## Closeout

- free space and inode headroom return within threshold
- any cleanup performed is documented
- any deferred operator action is captured explicitly
