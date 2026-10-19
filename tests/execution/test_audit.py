import unittest
from audit import classify

class Audit(unittest.TestCase):
    def test_exact_family_protocol_and_failed_attempts(self):
        good='connect(3<TCP:[42]>, {sa_family=AF_INET, sin_port=htons(80), sin_addr=inet_addr("93.184.216.34")}, 16) = -1 EINPROGRESS'
        self.assertFalse(classify([good])['violations'])
        for bad in (good.replace('93.184.216.34','169.254.169.254'),good.replace('TCP','UDP'),
                    'connect(3, {sa_family=AF_UNIX, sun_path="/var/run/nscd/socket"}, 110) = -1',
                    'sendmsg(3, {msg_name={sa_family=AF_UNKNOWN}}, 0) = -1'):
            self.assertTrue(classify([bad])['violations'])
    def test_ipv6_and_unknown(self):
        line='connect(3<TCPv6:[42]>, {sa_family=AF_INET6, sin6_port=htons(80), sin6_flowinfo=htonl(0), inet_pton(AF_INET6, "2606:4700:4700::1111", &sin6_addr), sin6_scope_id=0}, 28) = -1 EINPROGRESS'
        self.assertFalse(classify([line])['violations'])
        self.assertTrue(classify([line.replace('2606:4700:4700::1111','fd00::1')])['violations'])
        self.assertTrue(classify(['connect(3, ???, 16) = -1'])['violations'])
