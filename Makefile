REPONAME = collectors
VERSION = dev

.PHONY: all build_release

all: build_release

build_release:
	$(MAKE) -C ./files/zip
	$(MAKE) -C ./files/faban
	$(MAKE) -C ./dbms/mysql
	$(MAKE) -C ./environment/logs
	$(MAKE) -C ./environment/stats
	$(MAKE) -C ./environment/properties

build_container:
	$(MAKE) -C ./files/zip build_container
	$(MAKE) -C ./files/faban build_container
	$(MAKE) -C ./dbms/mysql build_container
	$(MAKE) -C ./environment/logs build_container
	$(MAKE) -C ./environment/stats build_container
	$(MAKE) -C ./environment/properties build_container

test: build_release
	$(MAKE) -C ./files/zip test
	$(MAKE) -C ./files/faban test
	$(MAKE) -C ./dbms/mysql test
	$(MAKE) -C ./environment/logs test
	$(MAKE) -C ./environment/stats test
	$(MAKE) -C ./environment/properties test
