"""Render the inactive AI overlay; no daemon, secret file or model call."""
import json
import os
from pathlib import Path
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]


class AIComposeCompatibilityTest(unittest.TestCase):
    def render(self, candidate=False, configured=False):
        command = [
            'docker', 'compose', '--profile', 'tools', '--env-file',
            str(ROOT / 'deploy/private/private.env.example'),
            '-f', str(ROOT / 'deploy/private/compose.yaml'),
            '-f', str(ROOT / 'deploy/ssh-management/compose.yaml'),
        ]
        if candidate:
            command += ['-f', str(ROOT / 'deploy/ssh-management/compose.ai.yaml.candidate')]
        command += ['config', '--format', 'json']
        # No local AI credentials/settings can enter this static fixture.
        env = {k: v for k, v in os.environ.items() if not k.startswith('AI_')}
        if configured:
            env.update(AI_BASE_URL='https://model-fixture.example/v1',
                       AI_MODEL='fixture-model', AI_TIMEOUT='20s')
        return subprocess.run(command, env=env, capture_output=True,
                              text=True, timeout=15)

    def test_candidate_is_api_only_and_keeps_network_boundary(self):
        base = self.render()
        candidate = self.render(candidate=True, configured=True)
        self.assertEqual(base.returncode, 0, base.stderr)
        self.assertEqual(candidate.returncode, 0, candidate.stderr)
        base = json.loads(base.stdout)
        value = json.loads(candidate.stdout)
        self.assertEqual(value['networks'], base['networks'])
        self.assertEqual(set(value['services']), set(base['services']))
        for name, service in value['services'].items():
            self.assertEqual(service.get('networks'), base['services'][name].get('networks'))
            self.assertEqual(service.get('ports'), base['services'][name].get('ports'))
            secrets = {s['source'] for s in service.get('secrets', [])}
            if name == 'api':
                self.assertEqual(secrets, {'app_url', 'ai_key'})
                self.assertEqual(service['environment']['AI_API_KEY'], '')
                self.assertEqual(service['environment']['AI_API_KEY_FILE'], '/run/secrets/ai_key')
                self.assertEqual(service['environment']['AI_BASE_URL'], 'https://model-fixture.example/v1')
            else:
                self.assertNotIn('ai_key', secrets)
        self.assertEqual(value['secrets']['ai_key']['file'],
                         str(ROOT / 'deploy/private/.secrets/ai_api_key'))

    def test_default_stays_disabled_and_candidate_requires_settings(self):
        base = self.render()
        self.assertEqual(base.returncode, 0, base.stderr)
        api = json.loads(base.stdout)['services']['api']
        self.assertFalse(any(k.startswith('AI_') for k in api['environment']))
        self.assertNotIn('ai_key', {s['source'] for s in api['secrets']})
        missing = self.render(candidate=True)
        self.assertNotEqual(missing.returncode, 0)
        self.assertIn('AI_BASE_URL', missing.stderr)


if __name__ == '__main__':
    unittest.main()
