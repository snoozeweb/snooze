#!/usr/bin/python36

from snooze.plugins.action import Action

class Script(Action):
    def __init__(self, core):
        super().__init__(core)

    def pprint(self, content):
        output = content.get('script')
        arguments = content.get('arguments')
        if arguments:
            output += ' ' + ' '.join(map(lambda x: '--'+x[0]+' '+x[1], arguments))
        return output

    def send(self, record, content):
        interpret_fields = content.get('interpret_fields')
        json = content.get('json')
        if content.interpret_fields:
            interpret_jinja(notification, record)
        arguments = record.copy()
        arguments.update({'notification': notification})
        script = [notification.action]
        arguments = notification.arguments
        for argument in arguments:
            if type(argument) is str:
                script.append(argument)
            if type(argument) is list:
                script += argument
            if type(argument) is dict:
                script += sum([ [k, v] for k, v in argument])
        try:
            log.debug("Will execute notification script `{}`".format(' '.join(script)))
            stdin = json.dumps(notification_arguments) if notification.json else None
            process = run(script, stdout=PIPE, input=stdin, encoding='ascii')
            log.debug('stdout: ' + str(process.stdout))
            log.debug('stderr: ' + str(process.stderr))
            self.core.stats.inc('notification_sent', {'name': name})
        except CalledProcessError as e:
            self.core.stats.inc('notification_error', {'name': name})
            log.error("Notification {} could not run `{}`. STDIN = {}, {}".format(
                name, ' '.join(script), stdin, e.output)
            )

    def interpret_jinja(notification, record):
        fields = notification.interpret_fields
        for field in fields:
            field_value = dig(notification, field)
            if field_value:
                interpreted = Template(field_value).render(record.copy())
                notification[field] = interpreted
                log.debug("Interpreting field {}: `{}` as `{}`".format(field, field_value, interpreted))
