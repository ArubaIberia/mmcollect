#/usr/bin/env python

################################################################
# Looks for controllers affected by bug #179415 (wrong nh)
#
# You have to dump the IP addresses and datapath tables of
# of controllers with something like:
#
# mkdir out
# mmcollect -h <mm IP> -u <username> -o out/ "show ip interface brief | $._data; show datapath session table | $._data"
#
# Then you can scan the output with
#
# python bug179415.py out
################################################################

import os
import sys

def findbug(fname, lines):
    """Looks for the bug in the output of show datapath session table

    The bug shows up when there is an entry with local source and destination
    addresses, but is being routed through a next-hop""" 
    interfaces = list()
    header = False
    for l in lines:
        if l.startswith("vlan "):
            parts = l.split()
            if parts[2] != "unassigned":
                octets = ".".join(parts[2].split(".")[:3])+"."
                interfaces.append(octets)
        elif "nh 0x" in l:
            parts = l.split()
            if any(parts[0].startswith(x) for x in interfaces) and any(parts[1].startswith(x) for x in interfaces):
                if not header:
                    print("\n***** FILE %s, IP INTERFACES: %s *****" % (fname, ", ".join(interfaces)))
                    header = True
                print("  "+l.strip())
    return 1 if header else 0

if __name__ == "__main__":
    total = 0
    if len(sys.argv) < 2:
        dirname = "out"
    else:
        dirname = sys.argv[1]
    for fname in os.listdir(dirname):
        if fname.endswith(".log"):
            with open(os.path.join(dirname, fname), "r") as infile:
                total += findbug(fname, infile.readlines())
    print("\n%d CONTROLLERS AFFECTED" % total)

