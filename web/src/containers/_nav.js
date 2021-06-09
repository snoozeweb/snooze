export default [
  {
    _name: 'CSidebarNav',
    _children: [
      {
        _name: 'CSidebarNavItem',
        name: 'Dashboard',
        to: '/dashboard',
        fontIcon: 'la la-tachometer',
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Alerts',
        to: '/record',
        fontIcon: 'la la-file-text',
	capabilities: ['ro_record', 'rw_record'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Aggregates',
        to: '/aggregate',
        fontIcon: 'la la-folder',
	capabilities: ['ro_aggregate', 'rw_aggregate'],
      },
      {
        _name: 'CSidebarNavTitle',
        _children: ['Core']
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Rules',
        to: '/rule',
        fontIcon: 'la la-balance-scale',
	capabilities: ['ro_rule', 'rw_rule'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Aggregate Rules',
        to: '/aggregaterule',
        fontIcon: 'la la-suitcase',
	capabilities: ['ro_aggregaterule', 'rw_aggregaterule'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Snooze',
        to: '/snooze',
        fontIcon: 'la la-bell-slash',
	capabilities: ['ro_snooze', 'rw_snooze'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Notifications',
        to: '/notification',
        fontIcon: 'la la-bullhorn',
	capabilities: ['ro_notification', 'rw_notification'],
      },
      {
        _name: 'CSidebarNavTitle',
        _children: ['Utils']
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Commands',
        to: '/command',
        fontIcon: 'la la-code',
	capabilities: ['ro_command', 'rw_command'],
      },
      {
        _name: 'CSidebarNavTitle',
        _children: ['Customize'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Users',
        to: '/user',
        fontIcon: 'la la-users',
	capabilities: ['ro_user', 'rw_user'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Roles',
        to: '/role',
        fontIcon: 'la la-user-friends',
	capabilities: ['ro_role', 'rw_role'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Settings',
        to: '/settings',
        fontIcon: 'la la-cog',
	capabilities: ['ro_settings', 'rw_settings'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Profile',
        to: '/profile',
        fontIcon: 'la la-sliders',
      },
    ]
  }
]
