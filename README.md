Agency Torturer
===============

This program tries 1296 different ways to start an ArangoDB agency with
3 servers. What is tried?

  - all 6 permutations of start orders
  - all 54 connected directed graphs of who gets the address of who else
    on the command line
  - all 4 possibilities of delays between the first and second, and the
    second and third server

For each way the program waits until all three answer to `_api/version`,
and then until all answer to `_api/agency/config` with a pool of 3 servers,
a list of 3 active ones, each knowing the same leader. 

Usage: Execute this program in the ArangoDB source tree with ArangoDB
       compiled in build/bin/arangod
